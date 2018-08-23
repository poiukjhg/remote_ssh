package main

import (
	"fmt"
	//    "io"
	"encoding/json"
	"github.com/garyburd/redigo/redis"
	"golang.org/x/crypto/ssh"
	"log"
	"net"
	"os"
	"reflect"
	"time"
	//    "strconv"
	//    "math/rand"
	"flag"
	"runtime"
	"strings"
	"syscall"
	"bytes"
    "encoding/binary"
    "encoding/hex"
    "regexp" 
    "sync" 
    "io"  
)

type out_put_res struct {
	//id         string
	session_id string
	message_id string
	host       string
	res_data   string
	local_len  int	
	state int
	closed bool
}

type Message struct {
	//id         string
	session_id string
	message_id string
	connect_type string
	host       string
	port       string
	username   string
	password   string
	cmd_data   string
	cmd_len    int
	closed     string
}

// session chan struct
type my_chan_s struct {
	in  chan Message
	out chan out_put_res
}

type my_session interface {
	StdinPipe() (io.WriteCloser, error) 
	StdoutPipe() (io.Reader, error)
	Close() error 
	RequestPty(term string, h, w int, termmodes ssh.TerminalModes) error
	Shell() error
	Wait() error 
}

var logger *log.Logger
/*
func logger.Printf(myfmt string, args ...string) {
	fmt.Printf(myfmt, args)
}
*/

// in chan map for all sessions, one k-v match one in chan, all sessions share one out chan
var my_chan_map map[string]my_chan_s
func checkError(err error, info string) {
	if err != nil {
		logger.Printf("%s. error: %s\n", info, err)
		os.Exit(1)
	}
}
//daemoned func
func daemon( /*nochdir, noclose int*/ ) int {
	var ret, ret2 uintptr
	var err syscall.Errno
	darwin := runtime.GOOS == "darwin"

	// already a daemon
	if syscall.Getppid() == 1 {
		return 0
	}

	// fork off the parent process
	ret, ret2, err = syscall.RawSyscall(syscall.SYS_FORK, 0, 0, 0)
	if err != 0 {
		return -1
	}

	// failure
	if ret2 < 0 {
		os.Exit(-1)
	}

	// handle exception for darwin
	if darwin && ret2 == 1 {
		ret = 0
	}

	// if we got a good PID, then we call exit the parent process.
	if ret > 0 {
		os.Exit(0)
	}

	/* Change the file mode mask */
	_ = syscall.Umask(0)

	// create a new SID for the child process
	s_ret, s_errno := syscall.Setsid()
	if s_errno != nil {
		log.Printf("Error: syscall.Setsid errno: %d", s_errno)
	}
	if s_ret < 0 {
		return -1
	}
	return 0
}

var regstr_more *regexp.Regexp
var regstr_end_tag *regexp.Regexp
func main() {		
	daemoned := flag.Bool("D", false, "Daemoned")
	redis_addr := flag.String("R", "127.0.0.1", "redis host")
	redis_port := flag.String("P", "6379", "redis port")
	redis_topic := flag.String("T", "topic", "redis topic")
	log_file := flag.String("L", "None", "log file name")
	flag.Parse()
	fmt.Println("Daemoned:", *daemoned)
	fmt.Println("log_file:", *log_file)
	if *daemoned == true {
		daemon()
	}
	if *log_file != "None"{
		logFile, err  := os.Create(*log_file)  
	    defer logFile.Close()  
	    if err != nil {  
	        log.Fatalln("open file error !")  
	    }  
		logger = log.New(logFile, "logger: ", log.Lshortfile|log.Ldate | log.Lmicroseconds )		
	} else {
		logger = log.New(os.Stdout, "logger: ", log.Lshortfile|log.Ldate | log.Lmicroseconds )		
	}

	regstr_more = regexp.MustCompile("[\t ]*[-]+[\t ]*More[\t ]*[-]+[\t ]*")
	host_root_str := "^\\[??[0-9A-Za-z|_]+@[0-9A-Za-z|:|/|[\t ]*|~]+\\]??#[\t ]*|\\[??[0-9A-Za-z|_]+@[0-9A-Za-z|:|/|~|[\t ]*]+\\]??#[\t ]*$|\\n\\[??[0-9A-Za-z|_]+@[0-9A-Za-z|:|/|~|[\t ]*]+\\]??#[\t ]*"	
	host_user_str := "^\\[??[0-9A-Za-z|_]+@[0-9A-Za-z|:|/|[\t ]*|~]+\\]??\\$[\t ]*|\\[??[0-9A-Za-z|_]+@[0-9A-Za-z|:|/|[\t ]*|~]+\\]??\\$[\t ]*$|\\n\\[??[0-9A-Za-z|_]+@[0-9A-Za-z|:|/|[\t ]*|~]+\\]??\\$[\t ]*"
	angle_str := "[\t ]*<.+>[\t ]*$|\\n[\t ]*<.+>[\t ]*"
	bracket_str := "^[\t ]*\\[.+\\][\t ]*|[\t ]*\\[.+\\][\t ]*:?[\t ]*$|\\n[\t ]*\\[.+\\][\t ]*"		
	//regstr_end_tag = regexp.MustCompile("^.+@.+\\$[\t ]*|\\n.+@.+\\$[\t ]*|^.+@.+#[\t ]*|\\n.+@.+#[\t ]*|^[\t ]*<.+>[\t ]*|^[\t ]*\\[.+\\][\t ]*|\\n[\t ]*<.+>[\t ]*|\\n[\t ]*\\[.+\\][\t ]*")		
	regstr_end_tag = regexp.MustCompile(host_root_str+"|"+host_user_str+"|"+angle_str+"|"+bracket_str)		
//redis chan
	redis_get_chan := make(chan string, 1024)
	send_redis_chan := make(chan out_put_res, 1024)
	var m map[string]interface{}
	var msg Message
//all in/out chans map to all session, key is session id
	my_chan_map = make(map[string]my_chan_s)

	go get_from_redis(redis_get_chan, *redis_addr, *redis_port, *redis_topic)
	go send_to_redis(send_redis_chan, *redis_addr, *redis_port)
	
	for {
		redis_res := <-redis_get_chan
		err := json.Unmarshal([]byte(redis_res), &m)
		if err != nil {
			logger.Printf("redis recv json error: %s\n", err)
			continue
		}
		//logger.Printf("redis recv json :%v \n", redis_res)
		//msg.id = m["id"].(string)	
		if reflect.ValueOf(m["session_id"]).IsValid() &&
			reflect.ValueOf(m["message_id"]).IsValid() &&
			reflect.ValueOf(m["host"]).IsValid() &&
			reflect.ValueOf(m["port"]).IsValid() &&
			reflect.ValueOf(m["username"]).IsValid() &&
			reflect.ValueOf(m["password"]).IsValid() &&
			reflect.ValueOf(m["cmd_data"]).IsValid() &&
			reflect.ValueOf(m["cmd_len"]).IsValid() &&
			reflect.ValueOf(m["closed"]).IsValid(){
				msg.session_id = m["session_id"].(string)		
				msg.message_id = m["message_id"].(string)
				msg.host = m["host"].(string)
				msg.port = m["port"].(string)
				msg.username = m["username"].(string)
				msg.password = m["password"].(string)
				msg.cmd_data = m["cmd_data"].(string)
				msg.cmd_len = int(m["cmd_len"].(float64))
				msg.closed = m["closed"].(string)
				msg.connect_type = m["connect_type"].(string)
		} else {
			logger.Printf("redis recv json error: %s\n", redis_res)			
			continue		
		}

		if v, ok := my_chan_map[msg.session_id]; ok {
			//session already existed
			v.in <- msg
		} else {
			if len(msg.host) > 0 {
				temp_in_chan := make(chan Message, 100)
				var my_chan_temp my_chan_s
				my_chan_temp.in = temp_in_chan
				my_chan_temp.out = send_redis_chan
				my_chan_map[msg.session_id] = my_chan_temp
				go session_helper(msg, my_chan_temp)
			}
		}
	}
}

func get_from_redis(redis_get_chan chan string, redis_host string, redis_port string, redis_topic string) {
	conn, err := redis.Dial("tcp", redis_host+":"+redis_port)
	if err != nil {		
		logger.Printf("Connect to redis error %v\n", err)
		return
	}
	defer conn.Close()

	psc := redis.PubSubConn{conn}
	psc.Subscribe(redis_topic)
	for {
		switch v := psc.Receive().(type) {
		case redis.Message:
			logger.Printf("Subscribe redis %s: message: %s\n", v.Channel, v.Data)
			redis_get_chan <- string(v.Data[:])
		case redis.Subscription:
			logger.Printf("Subscribe redis %s: %s %d\n", v.Channel, v.Kind, v.Count)
		case error:
			logger.Printf("Subscribe redis ,default error")
			os.Exit(-1)
		}
	}
}

func send_to_redis(send_redis_chan chan out_put_res, redis_host string, redis_port string) {
	conn, err := redis.Dial("tcp", redis_host+":"+redis_port)
	if err != nil {
		logger.Printf("Connect to redis error %v\n", err)
		return
	}
	defer conn.Close()
	regstr111 := regexp.MustCompile("\\x1b\\[[0-9]+A")
	regstr112 := regexp.MustCompile("\\x1b\\[[0-9]+B")
	regstr113 := regexp.MustCompile("\\x1b\\[[0-9]+C")
	regstr114 := regexp.MustCompile("\\x1b\\[[0-9]+D")
	regstr121 := regexp.MustCompile(" \\x1b\\[[0-9]+A")
	regstr122 := regexp.MustCompile(" \\x1b\\[[0-9]+B")
	regstr123 := regexp.MustCompile(" \\x1b\\[[0-9]+C")
	regstr124 := regexp.MustCompile(" \\x1b\\[[0-9]+D")
	regstr131 := regexp.MustCompile("\\^\\[\\[[0-9]+A")
	regstr132 := regexp.MustCompile("\\^\\[\\[[0-9]+B")
	regstr133 := regexp.MustCompile("\\^\\[\\[[0-9]+C")
	regstr134 := regexp.MustCompile("\\^\\[\\[[0-9]+D")
	for {
		cmd_res := <-send_redis_chan		
		cmd_res.res_data = regstr111.ReplaceAllString(cmd_res.res_data, "")		
		cmd_res.res_data = regstr112.ReplaceAllString(cmd_res.res_data, "")	
		cmd_res.res_data = regstr113.ReplaceAllString(cmd_res.res_data, "")	
		cmd_res.res_data = regstr114.ReplaceAllString(cmd_res.res_data, "")	
		cmd_res.res_data = regstr121.ReplaceAllString(cmd_res.res_data, "")		
		cmd_res.res_data = regstr122.ReplaceAllString(cmd_res.res_data, "")	
		cmd_res.res_data = regstr123.ReplaceAllString(cmd_res.res_data, "")	
		cmd_res.res_data = regstr124.ReplaceAllString(cmd_res.res_data, "")	
		cmd_res.res_data = regstr131.ReplaceAllString(cmd_res.res_data, "")		
		cmd_res.res_data = regstr132.ReplaceAllString(cmd_res.res_data, "")	
		cmd_res.res_data = regstr133.ReplaceAllString(cmd_res.res_data, "")	
		cmd_res.res_data = regstr134.ReplaceAllString(cmd_res.res_data, "")		
		//fmt.Printf("send str %v\n", cmd_res)
		//b := fmt.Sprintf("\"id\":%q, */\"session_id\":\"%s\", \"message_id\":\"%s\", \"host\":\"%s\", \"res_data\":\"%s\", \"local_len\":\"%d\", \"state\":\"%s\" ",
		//	/*cmd_res.id, */cmd_res.session_id, cmd_res.message_id, cmd_res.host, cmd_res.res_data, cmd_res.local_len, cmd_res.state)
		//b := fmt.Sprintf("\"session_id\":\"%s\", \"message_id\":\"%s\", \"host\":\"%s\", \"res_data\":\"%s\", \"local_len\":\"%d\" ",
		//	cmd_res.session_id, cmd_res.message_id, cmd_res.host, cmd_res.res_data, cmd_res.local_len)
		res_str := "0"		
		if cmd_res.closed == true{
			res_str = "1"
		}	
		//logger.Printf("+++++++++Send redis str %v\n", b)
		//logger.Printf("****************finally send %v res-- %v, %v\n", cmd_res.session_id, cmd_res.closed, res_str+"::"+cmd_res.res_data)
		//conn.Send("PUBLISH", "res", b)
	
		_, err = conn.Do("RPUSH", cmd_res.session_id, res_str+"::"+string([]byte(cmd_res.res_data)))
		checkError(err, "redis send failed")
		_, err = conn.Do("EXPIRE", cmd_res.session_id, 14400)
		conn.Flush()		
	}
}

//trim the last prompt line, like user@ubuntu:
func trim_last_line(in_string string) (string, string) {
	lines := strings.Split(in_string, "\n")
	slen := len(lines)
	if slen > 1 {
		s := lines[:slen-1]
		news := strings.Join(s, "\n")
		news = strings.Replace(news, "\r", "", -1)
		return news, strings.Join(lines[slen-1:], "\n")
	}
	return in_string, "" 
}

//main handler func, send to ssh and recv result string

func session_helper(base_msg Message, my_chan my_chan_s) {	
	logger.Printf("init session %s \n", base_msg.host)
	initiative_close := false
	in_chan := my_chan.in
	out_chan := my_chan.out
	var my_out_put_res out_put_res
	my_out_put_res.session_id = base_msg.session_id
	my_out_put_res.host = base_msg.host
	var (
		w	io.WriteCloser
		r	io.Reader
		err error		
		session my_session
	)

	err_handler_helper := func(e error, index_str string) { 
		logger.Printf("session error %s %s...%v\n", base_msg.host, e, index_str)          
		//var my_out_put_res out_put_res

		my_out_put_res.message_id = base_msg.message_id
		my_out_put_res.state = -2
		my_out_put_res.local_len = 15
		my_out_put_res.res_data = "##connect_failed"
		my_out_put_res.closed = true
		out_chan <- my_out_put_res	
		for cur_msg := range in_chan {	
			logger.Printf("connected failed %v", cur_msg.host)		
		}		
    }
    logger.Printf("......start to dial session %s ...\n", base_msg.host)
    if base_msg.connect_type=="SSH" {
    	logger.Printf("-------connect ssh....")
		config := &ssh.ClientConfig{
			User: base_msg.username,
			Auth: []ssh.AuthMethod{
				ssh.Password(base_msg.password),
			},
			HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
				return nil
			},
			BannerCallback: func(message string) error {
					// //welcome msg sync mutex	
					// var my_out_put_res out_put_res
					// //my_out_put_res.id = base_msg.id
					// my_out_put_res.state = true
					// my_out_put_res.session_id = base_msg.session_id
					// my_out_put_res.message_id = base_msg.message_id
					// my_out_put_res.host = base_msg.host
					// my_out_put_res.local_len = 0
					// my_out_put_res.res_data = message
					// logger.Printf("---------------connecting....... session %v\n\r", base_msg.session_id)
					// out_chan <- my_out_put_res			
				return nil
			},
			Timeout: time.Duration(10)*time.Second,
		}    
		clinet, err := ssh.Dial("tcp", base_msg.host+":"+base_msg.port, config)	  
		if err != nil {
			err_handler_helper(err, "1")
			return
		}
		
		logger.Printf("session dial ok: %s \n", base_msg.host)
		session, err = clinet.NewSession()
		if err != nil {
			err_handler_helper(err, "2")
			return
		}
		//defer session.Close()

		modes := ssh.TerminalModes{
			ssh.ECHO:          0,     // disable echoing
			ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
			ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
		}

		if err = session.RequestPty("default", 80, 40, modes); err != nil {
			err_handler_helper(err, "3")
			return
		}
	
    } else if base_msg.connect_type=="TELNET"{
    	logger.Printf("-------connect telnet....")    	
    	config := &TelnetConfig{
		    host : base_msg.host,  
		    port : base_msg.port,      
		    UserName : base_msg.username,  
		    Password : base_msg.password, 
		    Timeout : time.Duration(10)*time.Second,   		
    	}
    	if session, err = Telnet_Dial(*config); err != nil {
 			err_handler_helper(err, "4")
			return   		
    	}
    }
	if w, err = session.StdinPipe(); err != nil {
		err_handler_helper(err, "5")
		return     		
	}
	if r, err = session.StdoutPipe(); err != nil {
		err_handler_helper(err, "6")
		return     		
	}
		//e, err := session.StderrPipe()
		//checkError(err, "session err")

		/*
		   if err := session.Start("/bin/sh"); err != nil {
		       log.Fatal(err)
		   }
		*/	
	if err = session.Shell(); err != nil {
		err_handler_helper(err, "7")
		return
	}	    	
    

//close function, when closed gracefully, clear all resources directly, or sended closed msg then cleared
	defer func(){
		if initiative_close != true{
			//var my_out_put_res out_put_res			
			my_out_put_res.message_id = "-1"
			my_out_put_res.state = -3
			my_out_put_res.closed = true
			my_out_put_res.local_len = 15			
			out_chan <- my_out_put_res		
			logger.Printf("!!!!!!!!!session error closed, %s\n\r", base_msg.session_id)		
		}				
		w.Close()		
		logger.Printf("close pipe w %s\n\r", base_msg.session_id)			
		close(my_chan_map[base_msg.session_id].in)
		delete(my_chan_map, base_msg.session_id)
		logger.Printf("close_session %s\n\r", base_msg.session_id)	
		logger.Printf("delete map %s\n\r", base_msg.session_id)			

	}()

	var (
		read_buf            [1*1024 * 1024]byte
		tag_index              int			
		result string	
		mutex sync.Mutex	
		last_as_prefix string	
		//my_out_put_res out_put_res			
	)	
	/*read shell res , first msg is welcome news*/
	last_as_prefix = ""
	recv_helper := func (session_id string, message_id string) bool{	
		logger.Printf("session %s r start %s \n", base_msg.session_id, base_msg.host)
		tag_index=0	
		for {
			n, err := r.Read(read_buf[tag_index:])
			if err != nil {		
				initiative_close = false
				session.Close()	
				if err.Error() == "EOF" {
					my_out_put_res.res_data = fmt.Sprintf( "##session close")
				} else {
					my_out_put_res.res_data = fmt.Sprintf( "##session read error: %v", err)	
				}
				
				logger.Printf("---------exit pipe read error session %v %v\n\r", base_msg.session_id, err)
				return false
			}
			tag_index += n
			result = string(read_buf[:tag_index])
			if regstr_more.MatchString(result){
				logger.Printf("get More")
				result = regstr_more.ReplaceAllString(result, "\n")				
				copy(read_buf[0:tag_index], result)	
				w.Write([]byte("  "))
				continue
			}			
			if	regstr_end_tag.MatchString(result){					
				logger.Printf("recv match %v\n\r", result)
				break
			}			
		}
			// the last line is shell prompt, emit	
		mutex.Lock()	
		if tag_index == 0{
			return true
		}
		result = string(read_buf[:tag_index])											
		logger.Printf("-------------output session %s, last_as_prefix %s, msg id %s ,tag_index %v, res:%s", session_id, last_as_prefix, message_id, tag_index, result)	
		result, temp_str := trim_last_line(result)
		result = last_as_prefix+result
		last_as_prefix = temp_str
		my_out_put_res.state = 0
		my_out_put_res.message_id = message_id			
		my_out_put_res.closed = false
		my_out_put_res.local_len = tag_index
		my_out_put_res.res_data = result
		out_chan <- my_out_put_res	
		tag_index = 0
		mutex.Unlock()
		return true		
	}
	session_read_sync_chan := make(chan bool, 100)
	/* read-timeout func, if timeout, send cmd, then check the buff is not null and send it */
	go func(){
		read_timeout_long := time.NewTimer(20*time.Minute)
		read_timeout_soon := time.NewTimer(5*time.Minute)
		for {
			select{
				case session_read_sync_res := <-session_read_sync_chan:
					logger.Printf("~~~~~~~~~~~~~session_read_sync_res:%v\n",session_read_sync_res)
					if session_read_sync_res == true{
						read_timeout_long.Stop()
						read_timeout_soon.Stop()					
						return
					}
					read_timeout_long.Reset(20*time.Minute)
					read_timeout_soon.Reset(5*time.Minute)						
				case <-read_timeout_long.C:
					logger.Printf("!!!!!!!!long timeout\n")
					session.Close()
					initiative_close = false
					my_out_put_res.closed = true
					my_out_put_res.res_data = "##session read go timeout"
					return
				case <-read_timeout_soon.C:					
					read_timeout_soon.Reset(5*time.Minute)	
					mutex.Lock()
					if tag_index>0 {
						logger.Printf("!!!!!!!!!!!short timeout\n")
						select{
							case cur_msg, isOpened := <-in_chan:
								if !isOpened{
									my_out_put_res.res_data = "##go channel read error"				
									logger.Printf("--------in Channel closed %s %s\n\r", base_msg.session_id, base_msg.host)
									initiative_close = false	
									session.Close()	
									return	
								}
								in_cmd := cur_msg.cmd_data
							
								if cur_msg.closed == "close_session" {
									/*all news getted response, then close session gracefully*/	
									//var my_out_put_res out_put_res							
									my_out_put_res.state = 0				
									my_out_put_res.message_id = cur_msg.message_id				
									my_out_put_res.closed = true									
									my_out_put_res.local_len = 15
									my_out_put_res.res_data = "##closed"
									out_chan <- my_out_put_res	
									session.Close()
									initiative_close = true																			
									logger.Printf("^^^^^^^^close gracefully session %s %s\n\r", cur_msg.session_id, cur_msg.host)
									return
								}			
								logger.Printf("-------------input session %s, msg id %s ,shell cmd---%s ----", cur_msg.session_id, cur_msg.message_id, in_cmd)
								w.Write([]byte(in_cmd + "\n"))	
							default:															
						}						
						result = string(read_buf[0:tag_index])	
						result, temp_str := trim_last_line(result)
						result = last_as_prefix+result
						last_as_prefix = temp_str
						my_out_put_res.state = 0			
						my_out_put_res.message_id = "-1"			
						my_out_put_res.closed = false
						my_out_put_res.local_len = tag_index
						my_out_put_res.res_data = result
						out_chan <- my_out_put_res	
						tag_index = 0									
					}	
					mutex.Unlock()			
			}			
		}

	}()	
	/*send shell cmd*/		
	go func() {	
		defer func(){ 
			session_read_sync_chan<-true 
		}()			
		if recv_helper(base_msg.session_id, base_msg.message_id) == false{
			return
		}
		logger.Printf("session %s w start %s \n", base_msg.session_id, base_msg.host)							
		for {			
			cur_msg, isOpened := <-in_chan
			if !isOpened{
				my_out_put_res.res_data = "##go channel read error"				
				logger.Printf("--------in Channel closed %s %s\n\r", base_msg.session_id, base_msg.host)
				initiative_close = false	
				session.Close()	
				return	
			}
			in_cmd := cur_msg.cmd_data
		
			if cur_msg.closed == "close_session" {
				/*all news getted response, then close session gracefully*/	
				//var my_out_put_res out_put_res							
				my_out_put_res.state = 0				
				my_out_put_res.message_id = cur_msg.message_id				
				my_out_put_res.closed = true									
				my_out_put_res.local_len = 15
				my_out_put_res.res_data = "##closed"
				out_chan <- my_out_put_res	
				session.Close()
				initiative_close = true				
				time.Sleep(1 * time.Millisecond)								
				logger.Printf("^^^^^^^^close gracefully session %s %s\n\r", cur_msg.session_id, cur_msg.host)
				return
			}			
			logger.Printf("-------------input session %s, msg id %s ,shell cmd---%s ----", cur_msg.session_id, cur_msg.message_id, in_cmd)
			w_n, err := w.Write([]byte(in_cmd + "\n"))	
			if err != nil || w_n != strings.Count(in_cmd, ""){	
				initiative_close = false
				my_out_put_res.res_data = fmt.Sprintf( "##session write error: %v", err)
				session.Close()
				logger.Printf("---------exit pipe write error session %v %v\n\r", base_msg.session_id, err)
				return	
			}
			if recv_helper(base_msg.session_id, cur_msg.message_id) == false{
				return
			}
			session_read_sync_chan<-false
		}		
	}()
	session.Wait()
	logger.Printf("-------------exit session %v\n\r", base_msg.session_id)
}

func u2s(form string) (to string, err error) {
    bs, err := hex.DecodeString(strings.Replace(form, `\u`, ``, -1))
    if err != nil {
        return
    }
    for i, bl, br, r := 0, len(bs), bytes.NewReader(bs), uint16(0); i < bl; i += 2 {
        binary.Read(br, binary.BigEndian, &r)
        to += string(r)
    }
    return
}
