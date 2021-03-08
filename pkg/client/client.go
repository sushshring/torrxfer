package client

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/sushshring/torrxfer/pkg/net"
)

// TorrxferClient struct describes the client functionality for Torrxfer
type TorrxferClient struct {
	connections         []ServerConnection
	notificationChannel chan ServerNotification
	fileStoredDbs       []*FileWatcher
	mux                 sync.Mutex
}

func NewTorrxferClient() *TorrxferClient {
	// Initialize from local db
	c := new(TorrxferClient)
	c.fileStoredDbs = make([]*FileWatcher, 1)
	c.connections = make([]ServerConnection, 1)
	c.notificationChannel = make(chan ServerNotification)
	c.mux = sync.Mutex{}
	return c
}

// WatchDirectory watches a provided directory for changes and returns a channel the yields filepaths
func (client *TorrxferClient) WatchDirectory(dirname string) error {
	client.mux.Lock()
	defer client.mux.Unlock()
	fileWatcher, err := NewFileWatcher(dirname)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to create file watcher")
		return err
	}
	client.fileStoredDbs = append(client.fileStoredDbs, fileWatcher)
	// Start listening for files to be transferred
	go func() {
		for file := range fileWatcher.RegisterForFileNotifications() {
			if err := client.transferToServer(file.Path); err != nil {
				log.Debug().Err(err).Msg("File transfer to server failed")
				// Since file transfer failed, remove this from the DB so that a transfer is attempted at next run
				fileWatcher.RemoveWatchedFile(file.Path)
			}
		}
	}()
	return nil
}

// TransferToServer reads a provided file and transfers it to the connected servers
func (client *TorrxferClient) transferToServer(filename string) error {
	return nil
}

// ConnectServer creates a connection to the server provided
func (client *TorrxferClient) ConnectServer(server *common.ServerConnectionConfig) (*net.TorrxferServerConnection, error) {
	// Connect to the server
	rpc, err := net.NewTorrxferServerConnection(server)
	if err != nil {
		common.LogError(err, "RPC connection failed")
		return nil, err
	}
	// Update internal fields
	{
		client.mux.Lock()
		defer client.mux.Unlock()
		client.connections = append(client.connections, ServerConnection{
			Index:            uint16(len(client.connections) - 1),
			Address:          server.Address,
			Port:             server.Port,
			ConnectionTime:   time.Now(),
			BytesTransferred: 0,
			FilesTransferred: map[File]uint64{},
			rpcConnection:    rpc,
			mux:              sync.Mutex{},
		})
	}
	client.sendConnectionNotification(Connected, &client.connections[len(client.connections)-1])
	return rpc, nil
}

// RegisterForConnectionNotifications is a client method that notifies the caller on changes to all active connections
func (client *TorrxferClient) RegisterForConnectionNotifications() <-chan ServerNotification {
	return client.notificationChannel
}

func (client *TorrxferClient) sendConnectionNotification(n ConnectionNotificationType, updatedServer *ServerConnection) {
	client.notificationChannel <- ServerNotification{
		NotificationType: n,
		Connection:       updatedServer,
	}
}
