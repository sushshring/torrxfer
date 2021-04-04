package client

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/sushshring/torrxfer/pkg/net"
)

// ConnectionNotificationType is an iota for all types of connection notifications
type ConnectionNotificationType uint8

const (
	// ConnectionNotificationTypeConnected Connected
	ConnectionNotificationTypeConnected ConnectionNotificationType = iota
	// ConnectionNotificationTypeDisconnected Disconnected
	ConnectionNotificationTypeDisconnected
	// ConnectionNotificationTypeFilesUpdated Bytes transferred for file
	ConnectionNotificationTypeFilesUpdated
	// ConnectionNotificationTypeCompleted File transfer completed
	ConnectionNotificationTypeCompleted
	// ConnectionNotificationTypeQueryError Query error from server
	ConnectionNotificationTypeQueryError
	// ConnectionNotificationTypeTransferError Transfer error from server
	ConnectionNotificationTypeTransferError
)

// ConnectionNotificationStrings String representation of ConnectionNotificationType iota
var ConnectionNotificationStrings = map[ConnectionNotificationType]string{
	ConnectionNotificationTypeConnected:     "Connected",
	ConnectionNotificationTypeDisconnected:  "Disconnected",
	ConnectionNotificationTypeFilesUpdated:  "File Updated",
	ConnectionNotificationTypeQueryError:    "Query Error",
	ConnectionNotificationTypeTransferError: "Transfer Error",
	ConnectionNotificationTypeCompleted:     "Completed",
}

// ServerNotification is a struct that contains details about a notification from a server transfer action
type ServerNotification struct {
	NotificationType ConnectionNotificationType
	Error            error
	Connection       *ServerConnection
	SentFile         *File
}

// ServerConnection contains all active data about a connection with a Torrxfer server
type ServerConnection struct {
	index              uint16
	address            string
	port               uint32
	connectionTime     time.Time
	bytesTransferred   uint64
	fileTransferStatus map[File]uint64
	filesTransferred   map[string]File
	rpcConnection      net.TorrxferServerConnection

	sync.RWMutex
}

func newServerConnection(index uint16, address string, port uint32, rpcConnection net.TorrxferServerConnection) *ServerConnection {
	serverConnection := &ServerConnection{
		index:              index,
		address:            address,
		port:               port,
		connectionTime:     time.Now(),
		bytesTransferred:   0,
		fileTransferStatus: map[File]uint64{},
		filesTransferred:   map[string]File{},
		rpcConnection:      rpcConnection,
	}
	return serverConnection
}

// GetIndex returns the index of the server for the client
func (s *ServerConnection) GetIndex() (index uint16) {
	index = s.index
	return
}

// GetAddress returns the address of the server
func (s *ServerConnection) GetAddress() (address string) {
	address = s.address
	return
}

// GetPort returns the port of the server
func (s *ServerConnection) GetPort() (port uint32) {
	port = s.port
	return
}

// GetConnectionTime returns the time when the server was connected
func (s *ServerConnection) GetConnectionTime() (connectionTime time.Time) {
	connectionTime = s.connectionTime
	return
}

// GetBytesTransferred returns the total bytes transferred to the server in this session
func (s *ServerConnection) GetBytesTransferred() (bytesTransferred uint64) {
	s.RLock()
	defer s.RUnlock()
	bytesTransferred = s.bytesTransferred
	return
}

// GetFileSizeOnServer returns the current number of bytes transferred for this file in this session
func (s *ServerConnection) GetFileSizeOnServer(filename string) (fileSize uint64) {
	s.RLock()
	defer s.RUnlock()
	fileSize = s.fileTransferStatus[s.filesTransferred[filename]]
	return
}

// GetFilesTransferred returns the files transferred to the server in this session
func (s *ServerConnection) GetFilesTransferred() (files []File) {
	s.RLock()
	defer s.RUnlock()
	files = make([]File, len(s.filesTransferred))
	for _, v := range s.filesTransferred {
		files = append(files, v)
	}
	return
}

// MarshalZerologObject implements the zerolog Object Marshaller for easy connection detail logging
func (s *ServerConnection) MarshalZerologObject(e *zerolog.Event) {
	e.Str("Address", s.GetAddress()).Uint32("Port", s.GetPort()).Uint64("Bytes transferred", s.GetBytesTransferred())
}
