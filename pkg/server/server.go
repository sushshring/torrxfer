package server

import (
	"errors"
	"fmt"
	"io"
	gnet "net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
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

// TorrxferServer server struct
type TorrxferServer struct {
	activeFiles   map[string]*File
	serverRootDir string
	fileDb        db.KvDB
	sync.RWMutex
}

const (
	serverDbName string = "sfdb.dat"
)

// RunServer starts the server
func RunServer(serverConf common.ServerConfig, enableTLS bool, cafilePath, keyfilePath string) *TorrxferServer {
	lis, err := gnet.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", serverConf.Port))
	if err != nil {
		log.Fatal().Err(err).Msg("Could not start server")
	}
	var opts []grpc.ServerOption
	if enableTLS {
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
		activeFiles:   make(map[string]*File),
		fileDb:        db,
		serverRootDir: serverConf.SaveDir.Filepath,
	}
	rpcserver := net.NewRPCTorrxferServer(server)
	pb.RegisterRpcTorrxferServerServer(grpcServer, rpcserver)
	grpc.EnableTracing = true
	doneChan := server.configureSignals()
	go grpcServer.Serve(lis)
	<-doneChan
	grpcServer.Stop()
	for activeClient := range server.activeFiles {
		server.Close(activeClient)
	}
	server.fileDb.Close()
	return server
}

// QueryFunction implementation for gRPC call query file. Returns current file information and sets the file as a target for that connection clientID
func (s *TorrxferServer) QueryFunction(clientID string, file *net.RPCFile) (*net.RPCFile, error) {
	// Three cases:
	// Brand new file
	if !s.fileDb.Has(file.GetDataHash()) {
		log.Trace().Str("File name", file.GetFileName()).Msg("File not found in DB")

		// If a file with the name exists, remove it
		if _, err := os.Stat(s.getFullServerFilePath(file.GetMediaPath(), file.GetFileName())); err == nil {
			log.Trace().Err(err).Msg("File exists. Removing now")
			if err := os.Remove(s.getFullServerFilePath(file.GetMediaPath(), file.GetFileName())); err != nil {
				common.LogErrorStack(err, "File exists but could not remove")
				return nil, err
			}
		}
		// Add file to file DB
		readChan, writeChan := io.Pipe()
		// Set client's marked file to provided file
		serverFile := &File{
			fullPath:     s.getFullServerFilePath(file.GetMediaPath(), file.GetFileName()),
			mediaPrefix:  file.GetMediaPath(),
			size:         file.GetSize(),
			currentSize:  0,
			creationTime: time.Now(),
			modifiedTime: time.Now(),
			writeChannel: writeChan,
			readChannel:  readChan,
			errorChannel: make(chan error, 1),
			doneChannel:  make(chan struct{}, 1),
			mux:          fslock.Lock{},
			Cond: sync.Cond{
				L: &sync.RWMutex{},
			},
			RWMutex: sync.RWMutex{},
			Once:    sync.Once{},
		}
		bytes, err := serverFile.MarshalText()
		if err != nil {
			common.LogErrorStack(err, "Could not marshal file data")
			return nil, err
		}
		s.fileDb.Put(file.GetDataHash(), string(bytes))
		s.setActiveFile(clientID, file.GetDataHash(), serverFile)
		return serverFile.GenerateRPCFile()
	}

	log.Trace().Str("File name", file.GetFileName()).Msg("File found in DB")
	// File is either in transit or fully transferred
	currentFile := new(File)
	currentFileData, err := s.fileDb.Get(file.GetDataHash())
	if err != nil {
		log.Trace().Err(err).Msg("Could not get current file details, but file exists")
		return nil, err
	}
	log.Trace().Str("DB file data", currentFileData).Msg("Retrieved file data from DB")
	if err = currentFile.UnmarshalText([]byte(currentFileData)); err != nil {
		log.Trace().Err(err).Msg("Could not unmarshal file details")
		// Something funky happened when this file was last written. Best effort delete from db and return error
		// Let client retry the file transfer later
		s.fileDb.Delete(file.GetDataHash())
		return nil, err
	}

	stat, err := os.Stat(s.getFullServerFilePath(file.GetMediaPath(), file.GetFileName()))
	var fileSize int64
	var modifiedTime time.Time

	if err != nil {
		log.Trace().Err(err).Msg("Could not stat existing file")
		fileSize = 0
		modifiedTime = time.Unix(0, 0)
	} else {
		fileSize = stat.Size()
		modifiedTime = stat.ModTime()
	}
	readChan, writeChan := io.Pipe()
	serverFile := &File{
		fullPath:     s.getFullServerFilePath(file.GetMediaPath(), file.GetFileName()),
		mediaPrefix:  file.GetMediaPath(),
		size:         file.GetSize(),
		currentSize:  uint64(fileSize),
		creationTime: file.GetCreationTime(),
		modifiedTime: modifiedTime,
		writeChannel: writeChan,
		readChannel:  readChan,
		errorChannel: make(chan error, 1),
		doneChannel:  make(chan struct{}, 1),
		mux:          fslock.Lock{},
		Cond: sync.Cond{
			L: &sync.RWMutex{},
		},
		RWMutex: sync.RWMutex{},
		Once:    sync.Once{},
	}
	rpcFile, err := serverFile.GenerateRPCFile()
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

// TransferFunction gRPC TransferFile implementation. Writes the file bytes at the specified offset to the currently active file for the clientID
func (s *TorrxferServer) TransferFunction(clientID string, fileBytes []byte, blockSize uint32, currentOffset uint64) error {
	file := s.isFileActive(clientID)
	if file == nil {
		err := errors.New("no file active for client")
		common.LogErrorStack(err, clientID)
		return err
	}
	file.Once.Do(func() {
		file.currentSize = currentOffset
		file.Signal()
	})
	file.writeChannel.Write(fileBytes)
	return nil
}

// Close closes the active file for the clientID
func (s *TorrxferServer) Close(clientID string) {
	file := s.isFileActive(clientID)
	if file == nil {
		return
	}
	file.writeChannel.Close()
}

// RegisterForWriteNotification returns the notification channel for the clientID
func (s *TorrxferServer) RegisterForWriteNotification(clientID string) (chan error, chan struct{}) {
	file := s.isFileActive(clientID)
	if file == nil {
		return nil, nil
	}
	return file.errorChannel, file.doneChannel
}

func (s *TorrxferServer) isFileActive(clientID string) *File {
	s.RLock()
	defer s.RUnlock()

	if file, ok := s.activeFiles[clientID]; ok {
		return file
	}
	return nil
}

func (s *TorrxferServer) configureSignals() chan bool {
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		log.Trace().Str("Signal", sig.String()).Msg("Received int")
		done <- true
	}()
	return done
}

func (s *TorrxferServer) setActiveFile(clientID string, dbFileKey string, file *File) {
	s.Lock()
	defer s.Unlock()

	s.activeFiles[clientID] = file
	// Start file listener thread
	go s.startFileWriteThread(file, dbFileKey)
}

func (s *TorrxferServer) getFullServerFilePath(mediaPath, filename string) string {
	return filepath.Join(s.serverRootDir, mediaPath, filename)
}

func (s *TorrxferServer) startFileWriteThread(serverFile *File, dbFileKey string) {
	log.Trace().Str("Name", serverFile.fullPath).Msg("Starting writer thread")
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

	// Lock file on filesystem for writing
	serverFile.mux.Lock()
	defer serverFile.mux.Unlock()

	// Wait for Transfer func to get called first and set offset
	serverFile.L.Lock()
	serverFile.Wait()
	serverFile.L.Unlock()

	err = func() error {
		serverFile.RLock()
		defer serverFile.RUnlock()
		_, err = fileHandle.Seek(int64(serverFile.currentSize), 0)
		return err
	}()
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
