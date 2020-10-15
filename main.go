package main

import (
	"fmt"
	"net"
	"net/http"
	"path"
	"runtime"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/gwuhaolin/livego/configure"
	"github.com/gwuhaolin/livego/protocol/api"
	"github.com/gwuhaolin/livego/protocol/hls"
	"github.com/gwuhaolin/livego/protocol/httpflv"
	"github.com/gwuhaolin/livego/protocol/rtmp"
	log "github.com/sirupsen/logrus"
)

var VERSION = "master"

// func startHls() *hls.Server {
// 	hlsAddr := configure.Config.GetString("hls_addr")
// 	hlsListen, err := net.Listen("tcp", hlsAddr)
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	hlsServer := hls.NewServer()
// 	go func() {
// 		defer func() {
// 			if r := recover(); r != nil {
// 				log.Error("HLS server panic: ", r)
// 			}
// 		}()
// 		log.Info("HLS listen On ", hlsAddr)
// 		hlsServer.Serve(hlsListen)
// 	}()
// 	return hlsServer
// }

var rtmpAddr string

func startRtmp(stream *rtmp.RtmpStream, hlsServer *hls.Server) {
	rtmpAddr = configure.Config.GetString("rtmp_addr")

	rtmpListen, err := net.Listen("tcp", rtmpAddr)
	if err != nil {
		log.Fatal(err)
	}

	var rtmpServer *rtmp.Server

	if hlsServer == nil {
		rtmpServer = rtmp.NewRtmpServer(stream, nil)
		log.Info("HLS server disable....")
	} else {
		rtmpServer = rtmp.NewRtmpServer(stream, hlsServer)
		log.Info("HLS server enable....")
	}

	defer func() {
		if r := recover(); r != nil {
			log.Error("RTMP server panic: ", r)
		}
	}()
	log.Info("RTMP Listen On ", rtmpAddr)
	rtmpServer.Serve(rtmpListen)
}

func startHTTPFlv(stream *rtmp.RtmpStream) {
	httpflvAddr := configure.Config.GetString("httpflv_addr")

	flvListen, err := net.Listen("tcp", httpflvAddr)
	if err != nil {
		log.Fatal(err)
	}

	hdlServer := httpflv.NewServer(stream)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("HTTP-FLV server panic: ", r)
			}
		}()
		log.Info("HTTP-FLV listen On ", httpflvAddr)
		hdlServer.Serve(flvListen)
	}()
}

func startAPI(stream *rtmp.RtmpStream) {
	apiAddr := configure.Config.GetString("api_addr")

	if apiAddr != "" {
		opListen, err := net.Listen("tcp", apiAddr)
		if err != nil {
			log.Fatal(err)
		}
		opServer := api.NewServer(stream, rtmpAddr)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error("HTTP-API server panic: ", r)
				}
			}()
			log.Info("HTTP-API listen On ", apiAddr)
			opServer.Serve(opListen)
		}()
	}
}

func init() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			filename := path.Base(f.File)
			return fmt.Sprintf("%s()", f.Function), fmt.Sprintf(" %s:%d", filename, f.Line)
		},
	})
}

// 实现多房间聊天室思路
//创建map以roomName为key，访问者请求连接为value;value可以是map，也可以是切片数组;
//因为房间内有多个请求连接，为记录用户和请求，此处以用户id为key，请求为value;
// {roomid：{uid：conn}}
var rooms = make(map[string]map[int]*websocket.Conn)
var roomHosts = make(map[string]net.Conn)

var (
	// 服务器应用程序从HTTP请求处理程序调用Upgrader.Upgrade方法以获取* Conn;
	upgraderdd = websocket.Upgrader{
		// 读取存储空间大小
		ReadBufferSize: 1024,
		// 写入存储空间大小
		WriteBufferSize: 1024,
		// 允许跨域
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

// 连接websocket进入房间，首先在请求里指定要进入的房间
//  （也可以先进入个人房间，再转入其他房间都可以的，就看怎么处理连接对象conn，在这里先指定房间了）
// 所以在请求里需要带两个参数，房间名roomid和用户id
func wsEcho(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	roomid := r.Form["roomid"][0] //从请求里获取房价名roomid
	uidd := r.Form["uid"][0]      // 从请求里获取用户id
	uid, _ := strconv.Atoi(uidd)
	// conn就是建立连接后的连接对象
	conn, _ := upgraderdd.Upgrade(w, r, nil)

	defer conn.Close()

	func(conn *websocket.Conn) {
		if rooms[roomid] == nil {
			rooms[roomid] = map[int]*websocket.Conn{uid: conn}
		} else {
			rooms[roomid][uid] = conn
		}
	}(conn)

	for {
		// 某个链接发来数据data
		_, data, err := conn.ReadMessage()
		if err != nil {
			log.Info("It seems client has left... info =", err.Error())
			break
		}
		log.Info("msg from client : ", string(data))
		rooms[roomid][uid] = conn //把房间名和用户id及连接对象conn保存到map里
		// log.Info("roomid = ",roomid)
		//把数据返给当前房间里(除自己)所有的链接
		// for k,v :=range rooms[roomid]{
		// 	if k==uid{
		// 		continue
		// 	}
		// 	err := v.WriteMessage(msgTy,data)
		// 	if err!=nil{
		// 		log.Info("error:==",err)
		// 	}
		// 	log.Printf("Write msg to client: recved: %s \n",data)
		// }
		// for id,connhost := range roomHosts {
		// 	log.Info("id ",id, roomid, id == roomid)
		// 	if id == roomid{
		// 		log.Info("find room")
		// 		connhost.Write(data)
		// 	}
		// }
		connhost := roomHosts[roomid]
		if connhost != nil {
			log.Info("find room ", roomid)

			_, err := connhost.Write(data)
			if err != nil {
				log.Info("room ", roomid, " host left...")
				delete(roomHosts, roomid)
			}
		}

	}

}

func checkError(err error) {
	if err != nil {
		log.Info("error: ", err.Error())
	}
}

func startWS() {
	http.HandleFunc("/ws", wsEcho) // ws://127.0.0.1:11288/ws/?roomid=&uid=
	// 监听 地址 端口
	go func() {
		err := http.ListenAndServe(":11288", nil)
		if err != nil {
			log.Info("ListenAndServe error =", err.Error())
		}
	}()
}
func startTCP() {
	soIP := "192.168.252.39:12488"
	listen, err := net.Listen("tcp", soIP)
	checkError(err)
	defer listen.Close()
	for {
		log.Info("start socket")
		conn, err := listen.Accept()
		if err != nil {
			continue
		}
		buffer := make([]byte, 1024)
		connid, err := conn.Read(buffer)
		checkError(err)
		roomid := string(buffer[:connid])
		log.Info("roomid = " + roomid)
		roomHosts[roomid] = conn
		defer func(roomid string) {
			conn := roomHosts[roomid]
			if conn != nil {
				conn.Close()
				delete(roomHosts, roomid)
			}
		}(roomid)
	}
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Error("livego panic: ", r)
			time.Sleep(1 * time.Second)
		}
	}()

	log.Info("version: ", VERSION)

	stream := rtmp.NewRtmpStream()
	// hlsServer := startHls()
	startWS()
	go startTCP()
	startHTTPFlv(stream)
	startAPI(stream)
	startRtmp(stream, nil)
}
