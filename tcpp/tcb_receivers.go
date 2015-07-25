package tcpp

import (
	"github.com/hsheth2/logs"
)

func (c *TCB) packetDealer() {
	// read each tcp packet and deal with it
	logs.Trace.Println("Packet Dealer starting")
	for {
		//logs.Trace.Println("Waiting for packets")
		segment := <-c.read
		logs.Trace.Println("packetDealer received a packet:", segment, " in state:", c.state)

		// First check if closed, listen, or syn-sent state
		switch c.state {
		case CLOSED:
			logs.Trace.Println("Dealing closed")
			c.dealClosed(segment)
			continue
		case LISTEN:
			logs.Trace.Println("Dealing listen")
			c.dealListen(segment)
			continue
		case SYN_SENT:
			c.dealSynSent(segment)
			continue
		}

		// TODO check sequence number

		if segment.header.flags&TCP_RST != 0 {
			// TODO finish: page 70
			switch c.state {
			case SYN_RCVD:
				// TODO not done
				continue
			case ESTABLISHED, FIN_WAIT_1, FIN_WAIT_2, CLOSE_WAIT:
				// TODO not done
				continue
			case CLOSING, LAST_ACK, TIME_WAIT:
				if segment.header.flags&TCP_RST != 0 { // TODO why another if statement?
					c.UpdateState(CLOSED)
				}
				continue
			}
		}

		// TODO check security/precedence
		// TODO check SYN (SYN bit shouldn't be there)

		if segment.header.flags&TCP_ACK == 0 {
			logs.Info.Println("Dropping a packet without an ACK flag")
			continue
		}

		// now the segment must have an ACK flag

		switch c.state {
		case SYN_RCVD:
			c.dealSynRcvd(segment)
		case ESTABLISHED:
			if c.recentAckNum < segment.header.ack && segment.header.ack <= c.seqNum {
				c.UpdateLastAck(segment.header.ack)
				// TODO handle retransmission queue
				// TODO update send window
			} else if c.recentAckNum > segment.header.ack {
				// ignore
				logs.Info.Println("Dropping packet: ACK validation failed")
				continue
			} else if segment.header.ack > c.seqNum {
				// TODO send ack, drop segment, return
				logs.Info.Println("Dropping packet with bad ACK field")
				continue
			}
		case FIN_WAIT_1:
			// TODO check if acknowledging FIN
			c.UpdateState(FIN_WAIT_2)
		case FIN_WAIT_2:
		// TODO if retransmission queue empty, acknowledge user's close with ok
		case CLOSE_WAIT:
			if c.recentAckNum < segment.header.ack && segment.header.ack <= c.seqNum {
				c.recentAckNum = segment.header.ack
				// TODO handle retransmission queue
				// TODO update send window
			} else if c.recentAckNum > segment.header.ack {
				// ignore
				continue
			} else if segment.header.ack > c.seqNum {
				// TODO send ack, drop segment, return
			}
		case CLOSING:
			// TODO if ack is acknowledging our fin
			c.UpdateState(TIME_WAIT)
		// TODO else drop segment
		case LAST_ACK:
			// TODO if fin acknowledged
			c.UpdateState(CLOSED)
			continue
		case TIME_WAIT:
			// TODO handle remote fin
		}

		if segment.header.flags&TCP_URG != 0 {
			switch c.state {
			case ESTABLISHED, FIN_WAIT_1, FIN_WAIT_2:
				// TODO handle urg
			}
			continue
		}

		if segment.header.flags&TCP_FIN != 0 {
			switch c.state {
			case CLOSED, LISTEN, SYN_SENT:
				continue
			}

			// TODO notify user of the connection closing
			c.ackNum += segment.getPayloadSize()

			err := c.sendAck(c.seqNum, c.ackNum)
			logs.Info.Println("Sent ACK data in response to FIN")
			if err != nil {
				logs.Error.Println(err)
				continue
			}
			continue
		}

		switch c.state {
		case ESTABLISHED, FIN_WAIT_1, FIN_WAIT_2:
			c.recvBuffer = append(c.recvBuffer, segment.payload...)
			// TODO handle push flag
			// TODO adjust rcv.wnd, for now just multiplying by 2
			c.curWindow *= 2
			pay_size := segment.getPayloadSize()
			logs.Trace.Println("Payload Size is ", pay_size)

			// TODO piggyback this

			if pay_size > 1 { // TODO make this correct
				c.ackNum += pay_size
				err := c.sendAck(c.seqNum, c.ackNum)
				logs.Info.Println("Sent ACK data")
				if err != nil {
					logs.Error.Println(err)
					continue
				}
			}
			continue
		case CLOSE_WAIT, CLOSING, LAST_ACK, TIME_WAIT:
			// should not occur, so drop packet
			continue
		}
	}
}

func (c *TCB) dealClosed(d *TCP_Packet) {
	if d.header.flags&TCP_RST != 0 {
		return
	}
	var seqNum uint32
	var ackNum uint32
	rstFlags := uint8(TCP_RST)
	if d.header.flags&TCP_ACK == 0 {
		seqNum = 0
		ackNum = d.header.seq + d.getPayloadSize()
		rstFlags = rstFlags | TCP_ACK
	} else {
		seqNum = d.header.ack
		ackNum = 0
	}

	rst_packet := &TCP_Packet{
		header: &TCP_Header{
			seq:     seqNum,
			ack:     ackNum,
			flags:   rstFlags,
			urg:     0,
			options: []byte{},
		},
		payload: []byte{},
	}

	logs.Info.Printf("Sending RST data with seq %d and ack %d", seqNum, ackNum)
	err := c.sendPacket(rst_packet)
	if err != nil {
		logs.Error.Println(err)
		return
	}
}

func (c *TCB) dealListen(d *TCP_Packet) {
	if d.header.flags&TCP_RST != 0 {
		return
	}
	if d.header.flags&TCP_ACK != 0 {
		err := c.sendReset(d.header.ack, 0)
		logs.Trace.Println("Sent ACK data")
		if err != nil {
			logs.Error.Println(err)
			return
		}
	}

	if d.header.flags&TCP_SYN != 0 {
		// TODO check security/compartment, if not match, send <SEQ=SEG.ACK><CTL=RST>
		// TODO handle SEG.PRC > TCB.PRC stuff
		// TODO if SEG.PRC < TCP.PRC continue
		c.ackNum = d.header.seq + 1
		c.IRS = d.header.seq
		// TODO queue other controls

		syn_ack_packet := &TCP_Packet{
			header: &TCP_Header{
				seq:     c.seqNum,
				ack:     c.ackNum,
				flags:   TCP_SYN | TCP_ACK,
				urg:     0,
				options: []byte{},
			},
			payload: []byte{},
		}

		err := c.sendPacket(syn_ack_packet)
		if err != nil {
			logs.Error.Println(err)
			return
		}
		logs.Trace.Println("Sent ACK data")

		c.seqNum += 1
		c.recentAckNum = c.ISS
		c.UpdateState(SYN_RCVD)
		return
	}
}

func (c *TCB) dealSynSent(d *TCP_Packet) {
	logs.Trace.Println("Dealing state syn-sent")
	if d.header.flags&TCP_ACK != 0 {
		logs.Trace.Println("verifing the ack")
		if d.header.flags&TCP_RST != 0 {
			return
		}
		if d.header.ack <= c.ISS || d.header.ack > c.seqNum {
			logs.Info.Println("Sending reset")
			err := c.sendReset(d.header.ack, 0)
			if err != nil {
				logs.Error.Println(err)
				return
			}
			return
		}
		if !(c.recentAckNum <= d.header.ack && d.header.ack <= c.seqNum) {
			logs.Error.Println("Incoming packet's ack is bad")
			return
		}

		// kill the retransmission
		err := c.UpdateLastAck(d.header.ack)
		if err != nil {
			logs.Error.Println(err)
			return
		}
	}

	if d.header.flags&TCP_RST != 0 {
		logs.Error.Println("error: connection reset")
		c.UpdateState(CLOSED)
		return
	}

	// TODO verify security/precedence

	if d.header.flags&TCP_SYN != 0 {
		logs.Trace.Println("rcvd a SYN")
		c.ackNum = d.header.seq + 1
		c.IRS = d.header.seq

		if d.header.flags&TCP_ACK != 0 {
			c.UpdateLastAck(d.header.ack)
			logs.Trace.Println("recentAckNum:", c.recentAckNum)
			logs.Trace.Println("ISS:", c.ISS)
		}

		if c.recentAckNum > c.ISS {
			logs.Trace.Println("rcvd a SYN-ACK")
			// the syn has been ACKed
			// reply with an ACK
			err := c.sendAck(c.seqNum, c.ackNum)
			if err != nil {
				logs.Error.Println(err)
			}

			c.UpdateState(ESTABLISHED)
			logs.Info.Println("Connection established")
			return
		} else {
			// special case... TODO deal with this case later
			// http://www.tcpipguide.com/free/t_TCPConnectionEstablishmentProcessTheThreeWayHandsh-4.htm
			// (Simultaneous Open Connection Establishment)

			//c.UpdateState(SYN_RCVD)
			// TODO send <SEQ=ISS><ACK=RCV.NXT><CTL=SYN,ACK>
			// TODO if more controls/txt, continue processing after established
		}
	}

	// Neither syn nor rst set
	logs.Info.Println("Dropping packet with seq: ", d.header.seq, "ack: ", d.header.ack)
}

func (c *TCB) dealSynRcvd(d *TCP_Packet) {
	logs.Trace.Println("dealing Syn Rcvd")
	logs.Trace.Printf("recentAck: %d, header ack: %d, seqNum: %d", c.recentAckNum, d.header.ack, c.seqNum)
	if c.recentAckNum <= d.header.ack && d.header.ack <= c.seqNum {
		logs.Trace.Println("SynRcvd -> Established")
		c.UpdateState(ESTABLISHED)
	} else {
		err := c.sendReset(d.header.ack, 0)
		logs.Info.Println("Sent RST data")
		if err != nil {
			logs.Error.Println(err)
			return
		}
	}
}
