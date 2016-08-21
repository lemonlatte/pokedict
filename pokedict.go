package pokedict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
)

const (
	BOT_TOKEN    = ""
	PAGE_TOKEN   = ""
	FBMessageURI = "https://graph.facebook.com/v2.6/me/messages?access_token=" + PAGE_TOKEN
)

type FBObject struct {
	Object string
	Entry  []FBEntry
}

type FBEntry struct {
	Id        string
	Time      int64
	Messaging []FBMessage
}

type FBSender struct {
	Id string `json:"id"`
}

type FBRecipient struct {
	Id string `json:"id"`
}

type FBMessage struct {
	Sender    FBSender         `json:"sender,omitempty"`
	Recipient FBRecipient      `json:"recipient,omitempty"`
	Timestamp int64            `json:"timestamp,omitempty"`
	Content   FBMessageContent `json:"message"`
}

type FBMessageContent struct {
	Text string `json:"text"`
	Seq  int64  `json:"seq,omitempty"`
}

func init() {
	http.HandleFunc("/", handler)
	http.HandleFunc("/fbCallback", fbCBHandler)
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Hi, this is an FB Bot for PokéDict.")
}

func fbSendTextMessage(ctx context.Context, sender string, text string) (err error) {
	payload := FBMessage{
		Recipient: FBRecipient{sender},
		Content: FBMessageContent{
			Text: "hahahahah",
		},
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return
	}

	log.Infof(ctx, "FBMessageURI %s", FBMessageURI)
	log.Infof(ctx, "Payload %s", b)
	req, err := http.NewRequest("POST", FBMessageURI, bytes.NewBuffer(b))
	if err != nil {
		return
	}
	req.Header.Add("Content-Type", "application/json")

	tr := &urlfetch.Transport{Context: ctx}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		return
	}

	if resp.StatusCode != 200 {
		log.Infof(ctx, "Deliver status: %s", resp.Status)
		buffer := bytes.NewBuffer([]byte{})
		_, err = io.Copy(buffer, resp.Body)
		log.Infof(ctx, buffer.String())
	}
	return
}

func fbCBPostHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	ctx := appengine.NewContext(r)

	var fbObject FBObject
	d := json.NewDecoder(r.Body)
	err := d.Decode(&fbObject)

	if err != nil {
		log.Errorf(ctx, "%s", err.Error())
		http.Error(w, "unable to parse fb object from body", http.StatusInternalServerError)
	}

	fbMessages := fbObject.Entry[0].Messaging
	for _, fbMsg := range fbMessages {
		sender := fbMsg.Sender.Id
		if text := fbMsg.Content.Text; text != "" {
			err := fbSendTextMessage(ctx, sender, text)
			if err != nil {
				log.Errorf(ctx, "%s", err.Error())
				http.Error(w, "fail to deliver a message to a client", http.StatusInternalServerError)
			}
		}
	}
	fmt.Fprint(w, "")
}

func fbCBHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		if r.FormValue("hub.verify_token") == BOT_TOKEN {
			challenge := r.FormValue("hub.challenge")
			fmt.Fprint(w, challenge)
		} else {
			http.Error(w, "Invalid Token", http.StatusForbidden)
		}
	} else if r.Method == "POST" {
		fbCBPostHandler(w, r)
	} else {
		http.Error(w, "", http.StatusMethodNotAllowed)
	}
}
