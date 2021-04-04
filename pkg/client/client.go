package client

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/sushshring/torrxfer/pkg/crypto"
	"github.com/sushshring/torrxfer/pkg/net"
)

// TorrxferClient struct describes the client functionality for Torrxfer
type TorrxferClient interface {
	WatchDirectory(dirname, mediaDirectoryRoot string) error
	ConnectServer(server common.ServerConnectionConfig) (*ServerConnection, error)
	RegisterForConnectionNotifications() <-chan ServerNotification
	Run(*os.File) error
}

type torrxferClient struct {
	connections         []*ServerConnection
	notificationChannel chan ServerNotification
	fileStoredDbs       []FileWatcher
	sync.RWMutex
}

// NewTorrxferClient creates and instantiates a torrxfer client struct
func NewTorrxferClient() (c TorrxferClient) {
	// Initialize from local db
	c = &torrxferClient{make([]*ServerConnection, 0), make(chan ServerNotification, 500), make([]FileWatcher, 0), sync.RWMutex{}}
	return
}

func (c *torrxferClient) Run(config *os.File) error {
	jsonData, err := ioutil.ReadAll(config)
	if err != nil {
		common.LogErrorStack(err, "Failed to read config file")
		return err
	}

	var clientConfig common.ClientConfig
	err = json.Unmarshal(jsonData, &clientConfig)
	log.Debug().RawJSON("", jsonData).Msg("JSON data: ")
	if err != nil {
		common.LogErrorStack(err, "Failed to unmarshal json")
		return err
	}

	for _, serverConfig := range clientConfig.Servers {
		log.Debug().Str("Address", serverConfig.Address).Uint32("Port", serverConfig.Port).Msg("Connecting to server")
		server, err := c.ConnectServer(serverConfig)
		if err != nil {
			log.Error().Err(err).Str("Server address", serverConfig.Address).Msg("Failed to connect to server")
			continue
		}
		c.connections = append(c.connections, server)
	}

	for _, dir := range clientConfig.WatchedDirectories {
		err = c.WatchDirectory(dir.Directory, dir.MediaRoot)
		if err != nil {
			log.Error().Stack().Err(err).Str("Directory: ", dir.Directory).Msg("Could not watch directory")
		}
	}

	for notification := range c.RegisterForConnectionNotifications() {
		if notification.Error != nil {
			// Retry file transfer
			c.fileStoredDbs[0].RemoveWatchedFile(notification.SentFile.Path)
			// Touch the file to requeue a transfer
			os.Chtimes(notification.SentFile.Path, time.Now(), time.Now())
		}
		log.Info().
			Str("Notification: ", ConnectionNotificationStrings[notification.NotificationType]).
			Uint64("Bytes transferred: ", notification.Connection.bytesTransferred).
			Str("To: ", notification.Connection.address).
			Msg("Connection update")
	}
	return nil
}

// WatchDirectory watches a provided directory for changes and returns a channel the yields filepaths
// Currently the client does not support retroactively sending watched files to a new server connection
// If a new server connection is made, it will only get updates for files that are created or written to
// after the connection starts
func (c *torrxferClient) WatchDirectory(dirname, mediaDirectoryRoot string) error {
	log.Debug().Str("Adding directory", dirname).Send()
	fileWatcher, err := NewFileWatcher(dirname, mediaDirectoryRoot)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to create file watcher")
		return err
	}
	func() {
		c.Lock()
		defer c.Unlock()
		c.fileStoredDbs = append(c.fileStoredDbs, fileWatcher)
	}()
	// Start listening for files to be transferred
	go func() {
		for file := range fileWatcher.RegisterForFileNotifications() {
			log.Info().Str("Name", file.Path).Msg("Attempting to transfer file.")
			if err := c.transferToServers(file); err != nil {
				log.Debug().Err(err).Msg("File transfer to servers failed")
				// Since file transfer failed, remove this from the DB so that a transfer is attempted at next run
				fileWatcher.RemoveWatchedFile(file.Path)
				// Touch the file to requeue a transfer
				os.Chtimes(file.Path, time.Now(), time.Now())
			}
		}
	}()
	return nil
}

// ConnectServer creates a connection to the server provided
func (c *torrxferClient) ConnectServer(server common.ServerConnectionConfig) (*ServerConnection, error) {
	// Connect to the server
	rpc, err := net.NewTorrxferServerConnection(server)
	if err != nil {
		common.LogError(err, "RPC connection failed")
		return nil, err
	}

	serverConnection := newServerConnection(uint16(len(c.connections)), server.Address, server.Port, rpc)

	return serverConnection, nil
}

// RegisterForConnectionNotifications is a client method that notifies the caller on changes to all active connections
func (c *torrxferClient) RegisterForConnectionNotifications() <-chan ServerNotification {
	return c.notificationChannel
}

// transferToServers reads a provided file and transfers it to the connected servers
func (c *torrxferClient) transferToServers(file File) error {
	// Send file to all connected servers
	c.RLock()
	defer c.RUnlock()

	fileuuid := uuid.New()
	for _, server := range c.connections {
		log.Info().Str("Path", file.Path).Str("Address", server.address).Msg("Starting file transfer to server")
		// Prime the server for the file. This is a blocking call and the client will
		// hold a read lock until this completes. Any new connections or directories will not be
		// registered while a file transfer is starting
		file.TransferTime = time.Now()
		remoteFileInfo, err := server.rpcConnection.QueryFile(file.Path, file.MediaPrefix, fileuuid.String())
		if err != nil {
			c.sendConnectionNotification(ConnectionNotificationTypeQueryError, server, &file, err)
			// Currently, if a query fails on one connected server, the entire transfer is cancelled. The file is reattempted
			// when the client starts again.
			// TODO: More fine grained error handling for server-wise retry
			return err
		}
		// File was already fully transmitted
		// Verify based on data hash
		fileHash, err := crypto.HashFile(file.Path)
		if err != nil {
			// If hashing local file failed due to an transient error, just check for file size.
			// If file size is different, attempt to transfer again.
			fileHash = ""
		}
		if file.Size == remoteFileInfo.GetRemoteSize() || fileHash == remoteFileInfo.GetDataHash() {
			func() {
				server.Lock()
				defer server.Unlock()
				server.filesTransferred[file.Path] = file
				server.fileTransferStatus[file] = remoteFileInfo.GetSize()
				c.sendConnectionNotification(ConnectionNotificationTypeCompleted, server, &file)
			}()
			continue
		}

		// Server connection is primed for this file. Start sending now
		bytesReader, bytesWriter := io.Pipe()
		offset := remoteFileInfo.GetRemoteSize()

		// Setup the file transfer channels. Actual transferring is done on separate threads and should not
		// block the client functionality. There may be one blocking call but it should be fairly trivial
		fileSummaryChan, err := server.rpcConnection.TransferFile(bytesReader, common.DefaultBlockSize, offset, fileuuid.String())
		func() {
			server.Lock()
			defer server.Unlock()
			server.filesTransferred[file.Path] = file
		}()

		go func(dataChannel *io.PipeWriter, path string) {
			defer dataChannel.Close()
			// Open the file for reading
			fileOnDisk, err := os.Open(path)
			if err != nil {
				log.Debug().Err(err).Msg("Failed to open provided file")
				return
			}
			defer fileOnDisk.Close()
			n, err := io.Copy(dataChannel, fileOnDisk)
			if err != nil {
				common.LogErrorStack(err, "Failed to pipe from file")
				return
			}
			log.Debug().Int64("Written bytes", n).Msg("File transfer complete")
		}(bytesWriter, file.Path)

		go func(summaryChannel chan net.FileTransferNotification, server *ServerConnection, file *File) {
			for summary := range summaryChannel {
				switch summary.NotificationType {
				case net.TransferNotificationTypeError:
					c.sendConnectionNotification(ConnectionNotificationTypeTransferError, server, file, summary.Error)
					continue
				case net.TransferNotificationTypeBytes:
					func() {
						server.Lock()
						defer server.Unlock()
						server.bytesTransferred += summary.LastTransferred
						server.fileTransferStatus[*file] += summary.LastTransferred
						c.sendConnectionNotification(ConnectionNotificationTypeFilesUpdated, server, file)
					}()
					break
				case net.TransferNotificationTypeClosed:
					c.sendConnectionNotification(ConnectionNotificationTypeCompleted, server, file)
				}
			}
		}(fileSummaryChan, server, &file)
	}
	return nil
}

func (c *torrxferClient) sendConnectionNotification(n ConnectionNotificationType, updatedServer *ServerConnection, sentFile *File, err ...error) {
	serverNotif := ServerNotification{
		NotificationType: n,
		Error:            nil,
		Connection:       updatedServer,
		SentFile:         sentFile,
	}
	if len(err) != 0 {
		serverNotif.Error = err[0]
	}
	c.notificationChannel <- serverNotif
}
