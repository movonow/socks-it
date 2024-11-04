### Translation to English

**No Middleman**

There is no third-party middleman service. The `server` program starts a WebSocket endpoint acting as the middleman. Both the `client` and `server` will establish a connection to the WebSocket service via mTLS (Mutual TLS).

**Usage Instructions**

1. **Generate a Certificate Suite**
   The `generateSuite` program creates a CA certificate and generates both client and server certificates signed by this CA. It's crucial to ensure the IP address parameters for generating the certificate suite match the actual outgoing IP addresses for the proxy program as well as the `wsURL` parameter. Otherwise, TLS handshake failures may occur. The example commands assume that the client’s outgoing IP is `192.168.6.128` and the server’s outgoing IP is `192.168.6.1`. If negotiation errors occur, you can check the server logs to verify if the IP addresses are correct.

   ```bash
   generateSuite.exe -clientAddress 192.168.6.128 -serverAddress 192.168.6.1 -outputDir bin/certs
   ```

   The `clientAddress` and `serverAddress` parameters only support IP addresses, not domain names.

2. **Running the Client**
    - **On Linux (Shell)**

      ```bash
      client-linux-amd64 -wsURL wss://192.168.6.1:9443/ws \
          -proxyAddr 127.0.0.1:9015 \
          -caCertPath path/to/ca/cert \
          -clientCertPath path/to/client/cert \
          -clientKeyPath path/to/client/key
      ```

    - **On Windows (PowerShell)**

      ```powershell
      client-windows-amd64.exe -wsURL wss://192.168.6.1:9443/ws `
          -proxyAddr 127.0.0.1:9015 `
          -caCertPath path/to/ca/cert `
          -clientCertPath path/to/client/cert `
          -clientKeyPath path/to/client/key
      ```

3. **Running the Server**
    - **On Linux (Shell)**

      ```bash
      server-linux-amd64 -wsURL wss://192.168.6.1:9443/ws \
          -caCertPath path/to/ca/cert \
          -clientCertPath path/to/client/cert \
          -clientKeyPath path/to/client/key \
          -serverCertPath path/to/server/cert \
          -serverKeyPath path/to/server/key
      ```

    - **On Windows (PowerShell)**

      ```powershell
      server-windows-amd64.exe -wsURL wss://192.168.6.1:9443/ws `
          -caCertPath path/to/ca/cert `
          -clientCertPath path/to/client/cert `
          -clientKeyPath path/to/client/key `
          -serverCertPath path/to/server/cert `
          -serverKeyPath path/to/server/key
      ```