package main
import (
"net"
"golang.org/x/crypto/ssh"
"time"
"errors"
"io"
"regexp"
"strings"
)
//telnet protocol
//http://www.pcmicro.com/netfoss/telnet.html
const (
	// // SE End of subnegotiation parameters 240
	SE byte = '\xf0'
	// SB Indicates that what follows is subnegotiation of the indicated option 250
    SB byte = '\xfa'
    // WILL Indicates the desire to begin performing, or confirmation that you are now performing, the indicated option
    WILL byte = '\xfb'
    // WONT Indicates the refusal to perform, or continue performing, the indicated option
    WONT byte = '\xfc'
    // DO Indicates the request that the other party perform, or confirmation that you are expecting the other party to perform, the indicated option
    DO byte = '\xfd'
    // DONT Indicates the demand that the other party stop performing, or confirmation that you are no longer expecting the other party to perform, the indicated option
    DONT byte = '\xfe'
    // IAC Interpret as command
    IAC byte = '\xff'
    //ECHO Echo 1
    ECHO byte = '\x01'     
    // SGA Suppress go ahead 3
    SGA byte = '\x03'
    //CS client state 5
    CS byte = '\x05'
    // TT Terminal type 24
    TT byte = '\x18'
    // NAWS Negotiate about window size 31
    NAWS byte = '\x1f'
    // TS Terminal speed 32 
    TS byte = '\x20'
    //remote flow control 33
    RFC byte = '\x21'
    //Telnet X Display Location Optio
    XD byte = '\x35'
    //	New Environment
    NE byte = '\x39'
)

const (       
    TIME_DELAY_AFTER_WRITE = 300 //300ms  
)

type TelnetConfig struct {  	
    host             string  
    port             string      
    UserName         string  
    Password         string 
    Timeout          time.Duration  
} 

type Telnet_Session struct{
	config *TelnetConfig
	exitStatus chan bool
	Conn net.Conn
	is_closed bool
}

type sessionStdin struct {
	Conn net.Conn
	is_closed *bool		
}

func (s *sessionStdin) Close() error {
	if *(s.is_closed) == false{		
		*(s.is_closed) = true
		return s.Conn.Close()
	}
	return nil
}
func (s *sessionStdin) Write(b []byte) (n int, err error){
	return s.Conn.Write(b)
}


func (s *Telnet_Session)StdinPipe() (io.WriteCloser, error) {
	return &sessionStdin{s.Conn, &s.is_closed}, nil
}

func (s *Telnet_Session)StdoutPipe() (io.Reader, error){
	return s.Conn, nil
}

func (s *Telnet_Session)Close() error  {
	defer func(){
		s.is_closed = true
	}()
	s.exitStatus <- true
	return s.Conn.Close()
}

func (s *Telnet_Session) RequestPty(term string, h, w int, termmodes ssh.TerminalModes) error{
	return nil
}

func (s *Telnet_Session) Shell() error {
	host_root_str := ".+@.+#[\t ]*$"
	host_user_str := ".+@.+\\$[\t ]*$"
	angle_str := "[\t ]*<.+>[\t ]*$"
	bracket_str := "[\t ]*\\[.+\\][\t ]*:?[\t ]*$"		
	//regstr_end_tag = regexp.MustCompile("^.+@.+\\$[\t ]*|\\n.+@.+\\$[\t ]*|^.+@.+#[\t ]*|\\n.+@.+#[\t ]*|^[\t ]*<.+>[\t ]*|^[\t ]*\\[.+\\][\t ]*|\\n[\t ]*<.+>[\t ]*|\\n[\t ]*\\[.+\\][\t ]*")		
	regstr_end_tag = regexp.MustCompile(host_root_str+"|"+host_user_str+"|"+angle_str+"|"+bracket_str)	

	var buf [4096]byte 
	var i, n int
	var err error
	//var iac_len = 0

	for {
		if n, err = s.Conn.Read(buf[0:]); err != nil{
			return err
		} 	
		logger.Printf(">>>>>>>>>>>recv %v", buf[0:n])
		//if (n%3 == 0 && buf[0] == IAC) {
		i = 0
		if 	buf[i] == IAC {
			for{
				if (buf[i] == IAC && (buf[i+1] == DO || buf[i+1] == DONT || buf[i+1] == WILL || buf[i+1] == WONT)){
					switch buf[i+2] { 
						case ECHO : 
							if buf[i+1] == WILL {
								buf[i+1] = DO					
							}
							if buf[i+1] == DO {
								buf[i+1] = WILL					
							}
							if buf[i+1] == WONT {
								buf[i+1] = DONT					
							}
							if buf[i+1] == DONT {
								buf[i+1] = WONT					
							}
						case SGA : 
							if buf[i+1] == WILL {
								buf[i+1] = DO					
							}
							if buf[i+1] == DO {
								buf[i+1] = WILL					
							}
							if buf[i+1] == WONT {
								buf[i+1] = DONT					
							}
							if buf[i+1] == DONT {
								buf[i+1] = WONT					
							}						
						case CS: 
							buf[i+1] = DONT						
						case TT  : 						
						case NAWS: 						
						case TS  : 						
						case RFC : 						
						case XD:
						case NE:
							buf[i+1] = WONT	
					}	
					i+=2
				} 
				if buf[i] == IAC && buf[i+1] == SB && buf[i+4] == IAC && buf [i+5]==SE{
					if buf[i+2] == TT && buf[i+3] == ECHO{
						send_buf := [11] byte{IAC, SB, TT, 0, 76, 74, 31, 30, 30, IAC, SE}
						var tmp_buf []byte
						tmp_buf = append(tmp_buf, buf[0:i]...)
						tmp_buf = append(tmp_buf, send_buf[:]...)
						tmp_buf = append(tmp_buf,  buf[i+6:]...)					
						copy(buf[:], tmp_buf[0:])
						i+=5
					}
					i+=5			
				}
				if i>=n-1 {
					s.Conn.Write(buf[0:n] )
					logger.Printf("<<<<<<<<<<send cs %v", buf[0:n] )				
					break
				}
				i++				
				continue
			}	
			continue		
		}	
		


		// if (buf[0] == IAC && (buf[1] == DO || buf[1] == DONT || buf[1] == WILL || buf[1] == WONT)) {			
		// 	for i =0; i< n; i+=3{									
		// 		switch buf[i+2] { 
		// 			case ECHO : 
		// 				if buf[i+1] == WILL {
		// 					buf[i+1] = DO					
		// 				}
		// 				if buf[i+1] == DO {
		// 					buf[i+1] = WILL					
		// 				}
		// 				if buf[i+1] == WONT {
		// 					buf[i+1] = DONT					
		// 				}
		// 				if buf[i+1] == DONT {
		// 					buf[i+1] = WONT					
		// 				}
		// 			case SGA : 
		// 				if buf[i+1] == WILL {
		// 					buf[i+1] = DO					
		// 				}
		// 				if buf[i+1] == DO {
		// 					buf[i+1] = WILL					
		// 				}
		// 				if buf[i+1] == WONT {
		// 					buf[i+1] = DONT					
		// 				}
		// 				if buf[i+1] == DONT {
		// 					buf[i+1] = WONT					
		// 				}						
		// 			case CS: 
		// 				buf[i+1] = DONT						
		// 			case TT  : 						
		// 			case NAWS: 						
		// 			case TS  : 						
		// 			case RFC : 						
		// 			case XD:
		// 			case NE:
		// 				buf[i+1] = WONT	
		// 		}	
		
		// 		continue				
		// 	}
		// 	s.Conn.Write(buf[0:n] )
		// 	logger.Printf("<<<<<<<<<<send cs %v", buf[0:n] )
		// 	continue
		// }
		// if buf[0] == IAC && buf[1] == SB && buf[n-2] == IAC && buf [n-1]==SE {
		// 	if n==6 && buf[2] == TT && buf[3] == ECHO{
		// 		send_buf := [11] byte{IAC, SB, TT, 0, 76, 74, 31, 30, 30, IAC, SE}
		// 		s.Conn.Write(send_buf[:] )
		// 		logger.Printf("<<<<<<<<<<send cs %v", send_buf )			
		// 	}

		// 	continue
		// }	
		result := string(buf[0:n])
		if strings.Contains(result, "assword:") {
		    if _, err = s.Conn.Write([]byte(s.config.Password + "\n") );err != nil{
				return err
			} 	
			logger.Printf("<<<<<<<<<<send Password %v", s.config.Password)					
			break
		}
		if strings.Contains(result, "ogin:") || strings.Contains(result, "sername:")  {
		    if _, err = s.Conn.Write([]byte(s.config.UserName + "\n") );err != nil{
				return err
			} 
			logger.Printf("<<<<<<<<<<send UserName %v", s.config.UserName)	
			time.Sleep(TIME_DELAY_AFTER_WRITE * time.Millisecond)
			continue							
		}		
		if	regstr_end_tag.MatchString(result){		
			logger.Printf("<<<<<<<<<<match break %v")							
			break
		}			
	}  
    return nil
}

func (s *Telnet_Session) Wait() error {
	is_closed, chan_closed:= <-s.exitStatus
	if is_closed == true || chan_closed {
		return nil
	}
	close(s.exitStatus)
	return errors.New("wait error")
}

func Telnet_Dial(config TelnetConfig) (*Telnet_Session, error){
	var session Telnet_Session
	conn, err := net.DialTimeout("tcp", config.host+":"+config.port, config.Timeout)
	if err != nil {
		return &session, err
	}	
	session.Conn = conn
	session.is_closed = false	
	session.exitStatus = make(chan bool, 1)
	session.config = &config
	return &session, nil
}
