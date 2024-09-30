package sshtunnel

import (
	"errors"
	"golang.org/x/crypto/ssh"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

type SSHTunnel struct {
	sshConfig        *ssh.ClientConfig
	host             string
	sshClient        *ssh.Client
	netNetwork       string
	netAddress       string
	sshNetwork       string
	sshAddress       string
	listener         net.Listener
	closed           bool
	attemptSleepTime time.Duration
	ioTimeout        time.Duration
	logger           func(format string, v ...any)
	wg               *sync.WaitGroup
	mx               *sync.Mutex
}

func NewSSHTunnel(sshConfig *ssh.ClientConfig, host string, localPort string, remotePort string) *SSHTunnel {
	c := &SSHTunnel{
		sshConfig:        sshConfig,
		host:             host,
		sshClient:        nil,
		netNetwork:       "tcp",
		netAddress:       "localhost:" + localPort,
		sshNetwork:       "tcp",
		sshAddress:       "localhost:" + remotePort,
		listener:         nil,
		closed:           false,
		attemptSleepTime: 3 * time.Second,
		ioTimeout:        30 * time.Second,
		logger:           log.Printf,
		wg:               &sync.WaitGroup{},
		mx:               &sync.Mutex{},
	}
	return c
}

func (c *SSHTunnel) Close() {
	c.closed = true
	c.closeInternal()
	c.wg.Wait()
	c.log("Close successful")
}

func (c *SSHTunnel) SetAttemptSleepTime(sleepTime time.Duration) *SSHTunnel {
	c.attemptSleepTime = sleepTime
	return c
}

func (c *SSHTunnel) SetLogger(logger func(format string, v ...any)) *SSHTunnel {
	c.logger = logger
	return c
}

func (c *SSHTunnel) SetNoLogger() *SSHTunnel {
	return c.SetLogger(func(format string, v ...any) {})
}

func (c *SSHTunnel) log(format string, v ...any) {
	c.logger(format, v...)
}

func (c *SSHTunnel) closeInternal() {
	c.closeListener()
	c.closeSSH()
}
func (c *SSHTunnel) closeSSH() {
	if c.sshClient != nil {
		err := c.sshClient.Close()
		if err != nil && err != io.EOF {
			c.log("sshClient.Close failed: %v", err)
		}
		c.sshClient = nil
	}
	c.log("Closed sshClient")
}

func (c *SSHTunnel) closeListener() {
	if c.listener != nil {
		err := c.listener.Close()
		if err != nil && err != io.EOF {
			c.log("listener.Close failed: %v", err)
		}
		c.listener = nil
	}
	c.log("Closed listener")
}

func (c *SSHTunnel) sshConnect() error {
	defer c.mx.Unlock()
	c.mx.Lock()
	c.closeSSH()
	c.log("ssh.Connect...")
	client, err := ssh.Dial("tcp", c.host, c.sshConfig)
	if err != nil {
		c.log("ssh.Connect failed: %v", err)
		return err
	} else {
		c.log("ssh.Connect successful")
		c.sshClient = client
		return nil
	}
}

func (c *SSHTunnel) netDial() (net.Conn, error) {
	c.log("net.Dial to %s/%s...", c.netNetwork, c.netAddress)
	conn, err := net.Dial(c.netNetwork, c.netAddress)
	if err != nil {
		c.log("net.Dial failed: %v", err)
		return nil, err
	}
	c.log("net.Dial successful")
	return conn, nil
}

func (c *SSHTunnel) sshDial() (net.Conn, error) {
	c.log("ssh.Dial to %s/%s...", c.sshNetwork, c.sshAddress)
	if c.sshClient != nil {
		conn, err := c.sshClient.Dial(c.sshNetwork, c.sshAddress)
		if err != nil {
			_ = c.sshConnect()
			conn, err = c.sshClient.Dial(c.sshNetwork, c.sshAddress)
			if err != nil {
				c.log("ssh.Dial failed: %v", err)
				return nil, err
			}
		}
		c.log("ssh.Dial successful")
		return conn, nil
	}
	c.log("ssh.Dial failed: sshClient is nil")
	return nil, errors.New("sshClient is nil")
}

func (c *SSHTunnel) netListen() error {
	c.log("net.Listen on %s/%s...", c.netNetwork, c.netAddress)
	listener, err := net.Listen(c.netNetwork, c.netAddress)
	if err != nil {
		c.log("net.Listen failed: %v", err)
		return err
	}
	c.log("net.Listen successful")
	c.listener = listener
	return nil
}

func (c *SSHTunnel) sshListen() error {
	c.log("ssh.Listen on %s/%s...", c.sshNetwork, c.sshAddress)
	if c.sshClient != nil {
		listener, err := c.sshClient.Listen(c.sshNetwork, c.sshAddress)
		if err != nil {
			_ = c.sshConnect()
			listener, err = c.sshClient.Listen(c.sshNetwork, c.sshAddress)
			if err != nil {
				c.log("ssh.Listen failed: %v", err)
				return err
			}
		}
		c.log("ssh.Listen successful")
		c.listener = listener
		return nil
	}
	c.log("ssh.Listen failed: sshClient is nil")
	return errors.New("sshClient is nil")
}

func (c *SSHTunnel) accept() (net.Conn, error) {
	if c.listener != nil {
		conn, err := c.listener.Accept()
		if err == nil {
			c.log("accept successful")
			return conn, nil
		} else {
			c.log("accept failed: %v", err)
			return nil, err
		}
	}
	return nil, errors.New("listener is nil")
}

func (c *SSHTunnel) ForwardTunnel() {
	c.log("Starting Tunnel")

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		c.log("Started Tunnel")

		_ = c.sshConnect()

		for !c.closed {
			err := c.netListen()
			if err != nil {
				time.Sleep(c.attemptSleepTime)
				continue
			}

			for !c.closed {
				lConn, err := c.accept()
				if err != nil {
					c.closeListener()
					time.Sleep(c.attemptSleepTime)
					break
				}

				c.wg.Add(1)
				go func(lConn net.Conn) {
					defer c.wg.Done()

					rConn, err := c.sshDial()

					if err != nil {
						err = lConn.Close()
						if err != nil && err != io.EOF {
							c.log("lConn.Close failed: %v", err)
						}
						time.Sleep(c.attemptSleepTime)
						return
					}

					// set deadline for local connection
					err = lConn.SetDeadline(time.Now().Add(c.ioTimeout))
					if err != nil {
						c.log("SetDeadline failed: %v", err)
					}

					wg := &sync.WaitGroup{}
					wg.Add(2)
					go func(lConn net.Conn, rConn net.Conn) {
						defer wg.Done()
						// Push localPort to remotePort
						_, err := io.Copy(lConn, rConn)
						if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrDeadlineExceeded) {
							c.log("io.Copy (1 / local -> remote) failed: %v", err)
						}
					}(lConn, rConn)

					go func(lConn net.Conn, rConn net.Conn) {
						defer wg.Done()
						// Pull remotePort to localPort
						_, err := io.Copy(rConn, lConn)
						if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrDeadlineExceeded) {
							c.log("io.Copy (2 / remote -> local) failed: %v", err)
						}
					}(lConn, rConn)

					wg.Wait()

					err = lConn.Close()
					if err != nil && err != io.EOF {
						c.log("lConn.Close failed: %v", err)
					}
					err = rConn.Close()
					if err != nil && err != io.EOF {
						c.log("rConn.Close failed: %v", err)
					}
				}(lConn)
			}
		}

		c.log("Stopped Tunnel")
	}()
}

func (c *SSHTunnel) ReverseTunnel() {
	c.log("Starting Tunnel")

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		c.log("Started Tunnel")

		_ = c.sshConnect()

		for !c.closed {
			err := c.sshListen()
			if err != nil {
				time.Sleep(c.attemptSleepTime)
				continue
			}

			for !c.closed {
				rConn, err := c.accept()
				if err != nil {
					c.closeListener()
					time.Sleep(c.attemptSleepTime)
					break
				}

				c.wg.Add(1)
				go func(rConn net.Conn) {
					defer c.wg.Done()

					lConn, err := c.netDial()

					if err != nil {
						err = rConn.Close()
						if err != nil && err != io.EOF {
							c.log("rConn.Close failed: %v", err)
						}
						time.Sleep(c.attemptSleepTime)
						return
					}

					// set deadline for local connection
					err = lConn.SetDeadline(time.Now().Add(c.ioTimeout))
					if err != nil {
						c.log("SetDeadline failed: %v", err)
					}

					wg := &sync.WaitGroup{}
					wg.Add(2)
					go func(lConn net.Conn, rConn net.Conn) {
						defer wg.Done()
						// Push localPort to remotePort
						_, err := io.Copy(lConn, rConn)
						if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrDeadlineExceeded) {
							c.log("io.Copy (1 / local -> remote) failed: %v", err)
						}
					}(lConn, rConn)

					go func(lConn net.Conn, rConn net.Conn) {
						defer wg.Done()
						// Pull remotePort to localPort
						_, err := io.Copy(rConn, lConn)
						if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrDeadlineExceeded) {
							c.log("io.Copy (2 / remote -> local) failed: %v", err)
						}
					}(lConn, rConn)

					wg.Wait()

					err = lConn.Close()
					if err != nil && err != io.EOF {
						c.log("lConn.Close failed: %v", err)
					}
					err = rConn.Close()
					if err != nil && err != io.EOF {
						c.log("rConn.Close failed: %v", err)
					}
				}(rConn)
			}
		}

		c.log("Stopped Tunnel")
	}()
}

func (c *SSHTunnel) Wait() {
	c.wg.Wait()
}
