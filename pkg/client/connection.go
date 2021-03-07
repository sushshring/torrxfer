package client

import (
	"sync"
	"time"

	"github.com/sushshring/torrxfer/pkg/net"
)

type ConnectionNotificationType uint8

const (
	Connected ConnectionNotificationType = iota
	Disconnected
	BytesTransferred
	FilesUpdated
)

type ServerNotification struct {
	NotificationType ConnectionNotificationType
	Connection      *ServerConnection
}

type ServerConnection struct {
	Index            uint16
	Address          string
	Port             uint32
	ConnectionTime   time.Time
	BytesTransferred uint64
	FilesTransferred map[File]uint64
	rpcConnection    *net.TorrxferServerConnection

	mux sync.Mutex
}
