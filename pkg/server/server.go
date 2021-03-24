package server

import (
	"errors"
	"fmt"
	"io"
	gnet "net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/juju/fslock"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/internal/db"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/sushshring/torrxfer/pkg/net"
	pb "github.com/sushshring/torrxfer/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type TorrxferServer struct {
	activeFiles   map[string]*ServerFile
	serverRootDir string
	fileDb        db.KvDB
	sync.RWMutex
}

const (
	serverDbName string = "sfdb.dat"
)

func RunServer(serverConf common.ServerConfig, enableTls bool, cafilePath, keyfilePath string) *TorrxferServer {
	lis, err := gnet.Listen("tcp", fmt.Sprintf("localhost:%d", serverConf.Port))
	if err != nil {
		log.Fatal().Err(err).Msg("Could not start server")
	}
	var opts []grpc.ServerOption
	if enableTls {
		if cafilePath == "" {
			log.Fatal().Msg("CA File must be provided to run with TLS")
		}
		if _, err := os.Stat(cafilePath); os.IsNotExist(err) {
			log.Fatal().Msg("Valid CA file must be provided to run with TLS")
		}
		if keyfilePath == "" {
			log.Fatal().Msg("CA File must be provided to run with TLS")
		}
		if _, err := os.Stat(keyfilePath); os.IsNotExist(err) {
			log.Fatal().Msg("Valid CA file must be provided to run with TLS")
		}
		creds, err := credentials.NewServerTLSFromFile(cafilePath, keyfilePath)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to generate credentials")
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}

	db, err := db.GetDb(serverDbName)
	if err != nil {
		log.Fatal().Err(err).Msg("Could not initialize db")
	}
	grpcServer := grpc.NewServer(opts...)
	server := &TorrxferServer{
		activeFiles:   make(map[string]*ServerFile),
		fileDb:        db,
		serverRootDir: serverConf.SaveDir.Filepath,
	}
	rpcserver := net.NewRpcTorrxferServer(server)
	pb.RegisterRpcTorrxferServerServer(grpcServer, rpcserver)
	grpcServer.Serve(lis)
	return server
}

func (s *TorrxferServer) QueryFunction(clientID string, file *net.RPCFile) (*net.RPCFile, error) {
	// Three cases:
	// Brand new file
	if !s.fileDb.Has(file.GetDataHash()) {
		log.Debug().Str("File name", file.GetFileName()).Msg("File not found in DB")

		// If a file with the name exists, remove it
		if _, err := os.Stat(s.getFullServerFilePath(file.GetMediaPath(), file.GetFileName())); err == nil {
			log.Debug().Err(err).Msg("File exists. Removing now")
			if err := os.Remove(s.getFullServerFilePath(file.GetMediaPath(), file.GetFileName())); err != nil {
				common.LogErrorStack(err, "File exists but could not remove")
				return nil, err
			}
		}
		// Add file to file DB
		readChan, writeChan := io.Pipe()
		// Set client's marked file to provided file
		serverFile := &ServerFile{
			fullPath:     s.getFullServerFilePath(file.GetMediaPath(), file.GetFileName()),
			mediaPrefix:  file.GetMediaPath(),
			size:         file.GetSize(),
			currentSize:  0,
			creationTime: time.Now(),
			modifiedTime: time.Now(),
			readChannel:  readChan,
			writeChannel: writeChan,
			errorChannel: make(chan error, 1),
			doneChannel:  make(chan struct{}, 1),
			mux:          fslock.Lock{},
		}
		bytes, err := serverFile.MarshalText()
		if err != nil {
			common.LogErrorStack(err, "Could not marshal file data")
			return nil, err
		}
		s.fileDb.Put(file.GetDataHash(), string(bytes))
		s.setActiveFile(clientID, file.GetDataHash(), serverFile)
		return serverFile.GenerateRpcFile()
	} else {
		log.Debug().Str("File name", file.GetFileName()).Msg("File found in DB")
		// File is either in transit or fully transferred
		currentFile := new(ServerFile)
		currentFileData, err := s.fileDb.Get(file.GetDataHash())
		if err != nil {
			log.Debug().Err(err).Msg("Could not get current file details, but file exists")
			return nil, err
		}
		log.Debug().Str("DB file data", currentFileData).Msg("Retrieved file data from DB")
		if err = currentFile.UnmarshalText([]byte(currentFileData)); err != nil {
			log.Debug().Err(err).Msg("Could not unmarshal file details")
			// Something funky happened when this file was last written. Best effort delete from db and return error
			// Let client retry the file transfer later
			s.fileDb.Delete(file.GetDataHash())
			return nil, err
		}

		stat, err := os.Stat(s.getFullServerFilePath(file.GetMediaPath(), file.GetFileName()))
		var fileSize int64
		var modifiedTime time.Time

		if err != nil {
			log.Debug().Err(err).Msg("Could not stat existing file")
			fileSize = 0
			modifiedTime = time.Unix(0, 0)
		} else {
			fileSize = stat.Size()
			modifiedTime = stat.ModTime()
		}
		readChan, writeChan := io.Pipe()
		serverFile := &ServerFile{
			fullPath:     s.getFullServerFilePath(file.GetMediaPath(), file.GetFileName()),
			mediaPrefix:  file.GetMediaPath(),
			size:         file.GetSize(),
			currentSize:  uint64(fileSize),
			creationTime: file.GetCreationTime(),
			modifiedTime: modifiedTime,
			readChannel:  readChan,
			writeChannel: writeChan,
			errorChannel: make(chan error, 1),
			doneChannel:  make(chan struct{}, 1),
			mux:          fslock.Lock{},
		}
		rpcFile, err := serverFile.GenerateRpcFile()
		if err != nil {
			common.LogErrorStack(err, "Could not generate rpc representation")
			return nil, err
		}
		// File not fully transferred
		if rpcFile.GetDataHash() != file.GetDataHash() {
			s.setActiveFile(clientID, file.GetDataHash(), serverFile)
		}
		return rpcFile, nil
	}
}

func (s *TorrxferServer) TransferFunction(clientID string, fileBytes []byte, blockSize uint32, currentOffset uint64) error {
	file := s.isFileActive(clientID)
	if file == nil {
		err := errors.New("No file active for client")
		common.LogErrorStack(err, clientID)
		return err
	}
	file.writeChannel.Write(fileBytes)
	return nil
}

func (s *TorrxferServer) Close(clientID string) {
	file := s.isFileActive(clientID)
	if file == nil {
		return
	}
	file.writeChannel.Close()
}

func (s *TorrxferServer) RegisterForWriteNotification(clientID string) (chan error, chan struct{}) {
	file := s.isFileActive(clientID)
	if file == nil {
		return nil, nil
	}
	return file.errorChannel, file.doneChannel
}

func (s *TorrxferServer) isFileActive(clientID string) *ServerFile {
	s.RLock()
	defer s.RUnlock()

	if file, ok := s.activeFiles[clientID]; ok {
		return file
	}
	return nil
}

func (s *TorrxferServer) setActiveFile(clientID string, dbFileKey string, file *ServerFile) {
	s.Lock()
	defer s.Unlock()

	s.activeFiles[clientID] = file
	// Start file listener thread
	go s.startFileWriteThread(file, dbFileKey)
	return
}

func (s *TorrxferServer) getFullServerFilePath(mediaPath, filename string) string {
	return filepath.Join(s.serverRootDir, mediaPath, filename)
}

func (s *TorrxferServer) startFileWriteThread(serverFile *ServerFile, dbFileKey string) {
	log.Debug().Str("Name", serverFile.fullPath).Msg("Starting writer thread")
	defer close(serverFile.errorChannel)
	defer close(serverFile.doneChannel)

	if err := os.MkdirAll(filepath.Dir(serverFile.fullPath), 0755); err != nil {
		common.LogErrorStack(err, "Could not create file directory structure")
		serverFile.errorChannel <- err
		return
	}
	fileHandle, err := os.OpenFile(serverFile.fullPath, os.O_CREATE|os.O_RDWR, 0755)
	if err != nil {
		common.LogErrorStack(err, "Could not open server file for writing")
		serverFile.errorChannel <- err
		return
	}
	defer fileHandle.Close()
	serverFile.mux.Lock()
	defer serverFile.mux.Unlock()

	_, err = fileHandle.Seek(int64(serverFile.currentSize), 0)
	if err != nil {
		common.LogErrorStack(err, "Could not seek file to specified location")
		serverFile.errorChannel <- err
		return
	}

	n, err := io.Copy(fileHandle, serverFile.readChannel)
	if err != nil {
		common.LogErrorStack(err, "Write failed")
		serverFile.errorChannel <- err
		return
	}
	serverFile.currentSize += uint64(n)
	bytes, err := serverFile.MarshalText()
	if err != nil {
		common.LogErrorStack(err, "Could not marshal file data")
		serverFile.errorChannel <- err
		return
	}
	s.fileDb.Put(dbFileKey, string(bytes))
	//  File transfer is closed (may be complete or not)
	serverFile.doneChannel <- struct{}{}
}
