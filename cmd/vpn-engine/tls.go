package main

import (
	"crypto/tls"
	"net"
)

func newTLSClient(c net.Conn, serverName string) net.Conn {
	return tls.Client(c, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12})
}
