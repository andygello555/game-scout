package sock

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/gorilla/websocket"
	"sync"
)

type WebsocketMessageType string

const (
	ChangelistType  WebsocketMessageType = "Changelist"
	UsersOnlineType WebsocketMessageType = "UsersOnline"
)

func (wsmType WebsocketMessageType) String() string {
	return string(wsmType)
}

type WebsocketMessage struct {
	ChangeNumber int                  `json:"ChangeNumber"`
	Apps         map[string]string    `json:"Apps"`
	Packages     map[string]string    `json:"Packages"`
	Users        int                  `json:"Users"`
	Type         WebsocketMessageType `json:"Type"`
}

func (wsm *WebsocketMessage) String() string {
	return fmt.Sprintf(`Change no. %d
Users: %d
Apps: %v
Packages: %v
Type: %s
`, wsm.Users, wsm.ChangeNumber, wsm.Apps, wsm.Packages, wsm.Type.String())
}

func WebsocketClient(c *websocket.Conn, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		var msg WebsocketMessage
		err := c.ReadJSON(&msg)
		if err != nil {
			log.WARNING.Println("read:", err)
			return
		}

		if msg.Type == ChangelistType {
			log.INFO.Println(msg.String())
		}
	}
}
