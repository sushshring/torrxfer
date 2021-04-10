package client

import (
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/sushshring/torrxfer/pkg/crypto"
	"github.com/sushshring/torrxfer/pkg/net"
)

// ServerTransferJob holds the attributes needed to perform unit of work.
type ServerTransferJob struct {
	ID                    uuid.UUID
	Delay                 time.Duration
	ServerConnection      *ServerConnection
	File                  *File
	TransferNotifications chan ServerNotification
}

// NewServerTransferWorker creates takes a numeric id and a channel w/ worker pool.
func NewServerTransferWorker(id int, workerPool chan chan ServerTransferJob) ServerTransferWorker {
	return ServerTransferWorker{
		id:         id,
		jobQueue:   make(chan ServerTransferJob),
		workerPool: workerPool,
	}
}

// ServerTransferWorker performs the work necessary to transfer a file request to a server
type ServerTransferWorker struct {
	id         int
	jobQueue   chan ServerTransferJob
	workerPool chan chan ServerTransferJob
}

func (w ServerTransferWorker) doFileTransferJob(job ServerTransferJob) {
	file := job.File
	// Prime the server for the file.
	file.TransferTime = time.Now()
	remoteFileInfo, err := job.ServerConnection.rpcConnection.QueryFile(file.Path, file.MediaPrefix, job.ID.String())
	if err != nil {
		job.sendConnectionNotification(ConnectionNotificationTypeQueryError, 0, err)
		return
	}
	// File was already fully transmitted
	// Verify based on data hash
	fileHash, err := crypto.HashFile(file.Path)
	if err != nil {
		// If hashing local file failed due to an transient error, just check for file size.
		// If file size is different, attempt to transfer again.
		fileHash = ""
	}
	if fileHash == remoteFileInfo.GetDataHash() || file.Size == remoteFileInfo.GetRemoteSize() {
		func() {
			job.ServerConnection.Lock()
			defer job.ServerConnection.Unlock()
			job.ServerConnection.filesTransferred[file.Path] = file
			job.ServerConnection.fileTransferStatus[file] = remoteFileInfo.GetSize()
			job.sendConnectionNotification(ConnectionNotificationTypeCompleted, 0)
		}()
		return
	}

	// Server connection is primed for this file. Start sending now
	bytesReader, bytesWriter := io.Pipe()
	offset := remoteFileInfo.GetRemoteSize()

	// Open the file for reading
	fileOnDisk, err := os.Open(file.Path)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to open provided file")
		return
	}
	defer fileOnDisk.Close()
	// Continue transmission of file from last sent point if the data hash so far matches
	if offset > 0 {
		currentHash, err := crypto.HashReader(io.LimitReader(fileOnDisk, int64(offset)))
		if err != nil {
			// If error while generating hash, transfer the full file even though this was a local error
			common.LogErrorStack(err, "Could not generate hash of file to remote offset")
			currentHash = ""
		}
		if currentHash == remoteFileInfo.GetDataHash() {
			if _, err := fileOnDisk.Seek(int64(offset), 0); err != nil {
				// Could not seek to location locally. Transfer full file
				offset = 0
				// Best effort seek to 0. If failed,
				if _, err := fileOnDisk.Seek(0, 0); err != nil {
					job.sendConnectionNotification(ConnectionNotificationTypeFatalError, 0, err)
				}
			}
		}
	} else {
		offset = 0
		if _, err := fileOnDisk.Seek(0, 0); err != nil {
			job.sendConnectionNotification(ConnectionNotificationTypeFatalError, 0, err)
		}
	}
	// Setup the file transfer channels. There may be one blocking call but it should be fairly trivial
	fileSummaryChan, err := job.ServerConnection.rpcConnection.TransferFile(bytesReader, common.DefaultBlockSize, offset, job.ID.String())
	if err != nil {
		job.sendConnectionNotification(ConnectionNotificationTypeTransferError, 0, err)
		return
	}
	func() {
		job.ServerConnection.Lock()
		defer job.ServerConnection.Unlock()
		job.ServerConnection.filesTransferred[file.Path] = file
		job.ServerConnection.fileTransferStatus[file] = offset
	}()

	go func(summaryChannel chan net.FileTransferNotification, server *ServerConnection, file *File) {
		for summary := range summaryChannel {
			switch summary.NotificationType {
			case net.TransferNotificationTypeError:
				job.sendConnectionNotification(ConnectionNotificationTypeTransferError, 0, summary.Error)
				continue
			case net.TransferNotificationTypeBytes:
				func() {
					server.Lock()
					defer server.Unlock()
					server.bytesTransferred += summary.LastTransferred
					server.fileTransferStatus[file] += summary.LastTransferred
					job.sendConnectionNotification(ConnectionNotificationTypeFilesUpdated, summary.LastTransferred)
				}()
			case net.TransferNotificationTypeClosed:
				job.sendConnectionNotification(ConnectionNotificationTypeCompleted, 0)
			}
		}
	}(fileSummaryChan, job.ServerConnection, file)

	func(dataChannel *io.PipeWriter, path string) {
		defer dataChannel.Close()

		n, err := io.Copy(dataChannel, fileOnDisk)
		if err != nil {
			common.LogErrorStack(err, "Failed to pipe from file")
			job.sendConnectionNotification(ConnectionNotificationTypeTransferError, 0, err)
		}
		log.Trace().Int64("Written bytes", n).Msg("File transfer complete")
	}(bytesWriter, file.Path)

}

func (w ServerTransferJob) sendConnectionNotification(n ConnectionNotificationType, lastBlockSize uint64, err ...error) {
	serverNotif := ServerNotification{
		NotificationType: n,
		Error:            nil,
		Connection:       w.ServerConnection,
		SentFile:         w.File,
		LastSentSize:     lastBlockSize,
	}
	if len(err) != 0 {
		serverNotif.Error = err[0]
	}
	w.TransferNotifications <- serverNotif
}

func (w ServerTransferWorker) start() {
	for {
		// Add my jobQueue to the worker pool.
		w.workerPool <- w.jobQueue

		select {
		case job, ok := <-w.jobQueue:
			if ok {

				// Dispatcher has added a job to my jobQueue.
				log.Trace().Int("ID", w.id).Str("Job ID", job.ID.String()).Dur("Delay", job.Delay).Msg("Worker started")
				time.Sleep(job.Delay)
				w.doFileTransferJob(job)
			} else {
				// Job queue was closed. Exit
				return
			}
		}
	}
}
