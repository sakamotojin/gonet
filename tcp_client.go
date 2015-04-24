package main

import (
	"fmt"
	"golang.org/x/net/ipv4"
	"net"
	"errors"
	"time"
)

func New_TCB_From_Client(local, remote uint16, dstIP string) (*TCB, error) {
	/*write, err := NewIP_Writer(dstIP, TCP_PROTO)
	if err != nil {
		return nil, err
	}*/

	read, err := TCP_Port_Manager.bind(remote, local, dstIP)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	p, err := net.ListenPacket(fmt.Sprintf("ip4:%d", TCP_PROTO), dstIP) // only for read, not for write
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	r, err := ipv4.NewRawConn(p)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	fmt.Println("Finished New TCB from Client")
	return New_TCB(local, remote, dstIP, read, r, TCP_CLIENT)
}

func (c *TCB) Connect() error {
	if c.kind != TCP_CLIENT || c.state != CLOSED {
		return errors.New("TCB is not a closed client")
	}
	// Send the SYN packet
	SYN, err := (&TCP_Header{
		srcport: c.lport,
		dstport: c.rport,
		seq:     c.seqNum,
		ack:     c.ackNum,
		flags:   TCP_SYN,
		window:  c.curWindow, // TODO improve the window size calculation
		urg:     0,
		options: []byte{0x02, 0x04, 0xff, 0xd7, 0x04, 0x02, 0x08, 0x0a, 0x02, 0x64, 0x80, 0x8b, 0x0, 0x0, 0x0, 0x0, 0x01, 0x03, 0x03, 0x07}, // TODO compute the options of SYN instead of hardcoding them
	}).Marshal_TCP_Header(c.ipAddress, c.srcIP)
	if err != nil {
		return err
	}

	//c.writer.WriteTo(SYN)
	err = MyRawConnTCPWrite(c.writer, SYN, c.ipAddress)
	fmt.Println("Sent SYN")
	if err != nil {
		return err
	}
	c.UpdateState(SYN_SENT)

	// TODO set up resend SYN timers

	// wait for the connection state to be ready
	// TODO use sync.Cond broadcast to avoid the infinite for loop
	for {
		st := c.state
		if st == CLOSED {
			return errors.New("Connection timed out and closed, or reset.")
		} else if st == ESTABLISHED {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}