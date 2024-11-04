# Socks it

This project implements SOCKS5 proxy functionality. By implementing the [Middleman](proxy/middleman.go) interface, you can customize data transmission methods.

```text
                     Client Network        │        Server Network         
                                           │                              
┌──────────────────────┐                   │                  ┌─────────────────────────┐ 
│                      │                   │                  │                         │ 
│  Client (Browser...) │                   │                  │  Server (Web Server...) │ 
│                      │                   │                  │                         │ 
└────┬─▲───────────────┘                   │                  └──────────────────┬─▲────┘ 
     │ │                             ┌─────┴─────┐                               │ │      
     │ │                             │ Middleman │                               │ │      
     │ │                             │  Service  │                               │ │      
┌────┼─┼────────────────┐            └┬──▲───┬──▲┘            ┌──────────────────┼─┼────┐ 
│    │ │                │             │  │   │  │             │                  │ │    │ 
│ ┌──▼─┴─┐   ┌────────┐ │             │  │   │  │             │ ┌─────────┐    ┌─▼─┴──┐ │ 
│ │Socks5│◄──│ Tunnel │◄┼──Transport──┘  │   │  └──Transport──┼─┤ Tunnel  │◄───│Stream│ │ 
│ │Proxy │──►│ Client │─┼──Transport─────┘   └─────Transport──┼►│ Server  │───►│Proxy │ │ 
│ └──────┘   └────────┘ │                                     │ └─────────┘    └──────┘ │ 
│                       │                                     │                         │ 
└───────────────────────┘                                     └─────────────────────────┘ 
     proxy/bin/client                                             proxy/bin/server
```

---

## Names

Consistent naming can enhance the efficiency of information transmission. This project uses the following names.

### Proxy

The proxy is divided into two parts:

1. **Client Side**: Listens for SOCKS5 services, establishing connections and forwarding client data through the transport service request Stream Proxy.
2. **Server Side**: Listens for SOCKS5 commands and acts as a client to directly initiate TCP connection requests to the target service and forward data.

### Client Side

This is the area of the network that users can access directly and must go through the proxy to access the server side. The [proxy/bin/client](proxy/bin/client) program is run to start the SOCKS5 proxy listening service. Client tools such as browsers and Navicat should be configured to point their SOCKS5 proxy address to the listening port.

### Server Side

This network area is typically not directly reachable by users and must be accessed through the proxy service. The [proxy/bin/server](proxy/bin/server) program listens for stream access requests. The server initiates connections to the target (web service, database) and sends and receives data.

### Middleman

This is a service accessible from both the client and server sides that forwards data on behalf of the actual users, such as a chat system or a comments section that can be accessed both internally and externally. A data transmission channel is created through the `NewTransport` interface.

### Transport Layer

This is the data transmission channel between tools and the Middleman, such as the methods for sending and receiving IM messages in a chat system or the read/write interfaces in a comment system. The `NewTransport` interface of [Middleman](proxy/middleman.go) creates the data transmission interface between tools and the Middleman, as detailed in the [Middleman](middlemen) implementation.

### Tunnel

When a browser accesses a page, it typically initiates multiple TCP connection requests to different servers for various resources, with each connection corresponding to a Tunnel. The [Tunnel](proxy/bin/internal/tunnel.go) is an abstraction built on top of [Transport](proxy/transport.go), where all Tunnels usually share a single Transport instance.

---

This translation maintains the structure and details of the original text while making it accessible to an English-speaking audience. If you need further modifications or additional information, feel free to ask!