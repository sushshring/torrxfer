package client

import (
	"sync"
	"time"

	"github.com/sushshring/torrxfer/pkg/net"
)

type ConnectionNotificationType uint8

const (
	ConnectionNotificationTypeConnected ConnectionNotificationType = iota
	ConnectionNotificationTypeDisconnected
	ConnectionNotificationTypeFilesUpdated
	ConnectionNotificationTypeCompleted
	ConnectionNotificationTypeQueryError
	ConnectionNotificationTypeTransferError
)

var ConnectionNotificationStrings = map[ConnectionNotificationType]string{
	ConnectionNotificationTypeConnected:     "Connected",
	ConnectionNotificationTypeDisconnected:  "Disconnected",
	ConnectionNotificationTypeFilesUpdated:  "File Updated",
	ConnectionNotificationTypeQueryError:    "Query Error",
	ConnectionNotificationTypeTransferError: "Transfer Error",
	ConnectionNotificationTypeCompleted:     "Completed",
}

type ServerNotification struct {
	NotificationType ConnectionNotificationType
	Error            error
	Connection       *ServerConnection
	sentFile         *ClientFile
}

type ServerConnection struct {
	index              uint16
	address            string
	port               uint32
	connectionTime     time.Time
	bytesTransferred   uint64
	fileTransferStatus map[ClientFile]uint64
	filesTransferred   map[string]ClientFile
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
		fileTransferStatus: map[ClientFile]uint64{},
		filesTransferred:   map[string]ClientFile{},
		rpcConnection:      rpcConnection,
	}
	return serverConnection
}

func (s *ServerConnection) GetIndex() (index uint16) {
	index = s.index
	return
}

func (s *ServerConnection) GetAddress() (address string) {
	address = s.address
	return
}

func (s *ServerConnection) GetPort() (port uint32) {
	port = s.port
	return
}

func (s *ServerConnection) GetConnectionTime() (connectionTime time.Time) {
	connectionTime = s.connectionTime
	return
}

func (s *ServerConnection) GetBytesTransferred() (bytesTransferred uint64) {
	s.RLock()
	defer s.RUnlock()
	bytesTransferred = s.bytesTransferred
	return
}

func (s *ServerConnection) GetFileSizeOnServer(filename string) (fileSize uint64) {
	s.RLock()
	defer s.RUnlock()
	fileSize = s.fileTransferStatus[s.filesTransferred[filename]]
	return
}

func (s *ServerConnection) GetFilesTransferred() (files []ClientFile) {
	s.RLock()
	defer s.RUnlock()
	files = make([]ClientFile, len(s.filesTransferred))
	for _, v := range s.filesTransferred {
		files = append(files, v)
	}
	return
}
