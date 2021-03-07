# Torrxfer
### A simple directory scanner client and hosting server to copy/move torrent downloads

## Design
### Client
The client is a simple command-line application that runs on the intended torrent download system. It watches over a number of directories and transfers its contents to the connected server(s).

#### Operations
- Directory watch
    
    Provided a list of directories, the client watches them for any changes and triggers a copy from the server
    - Library methods:
        ```go
        // Watch a provided directory for changes and returns a channel the yields filepaths
        func (client *TorrxferClient) WatchDirectory(dirname string) (<-chan string, error)
        // Transfer data to connected servers
        func (client *TorrxferClient) TransferToServer(servers TorrxferServerConnection[], filename string) error
        ```
- Server connections

    Connect to an active server
    - Library methods:
        ```go
        // Connect to a server that is listening for new file transfers
        func (client *TorrxferClient) ConnectServer(server *ServerConf) (serverConnection *TorrxferServerConnection, error)
        // Query server whether the file is already transferred
        func (client *TorrxferServerConnection) QueryFile(filePath string) (FileSummary, error)
        ```
    - Types:
        ```go
        type ServerConf struct {
            Address string
            Port string
            DbFile string
        }
        ```

### Server
The server works as a gRPC service and accepts connections from authenticated clients. Clients may make gRPC calls to the server to request functionality listed below. The job of the server is to accept incoming files and stage them for the media manager application.

Any file that is transferred to the server is stored in a common staging location, at which point the media management application can pick it up and move it to the correct location.

#### Operations
- Client connections
    - Service methods:
        ```go
        service TorrxferServer {
            rpc TransferFile(File, stream byte) returns (FileSummary) {}
            rpc QueryFile(File) returns (FileSummary) {}
        }
        ```

- File Storage
    - Library methods:
        ```go
        // Create or import database of transferred files
        func (server *TorrxferServer) LoadFileDb(serverConf *ServerConf) error

        // Query file database for existing file summary
        func (server *TorrxferServer) QueryFileDb(fileHash string) (fileSummary *FileSummary, error)
        ```


