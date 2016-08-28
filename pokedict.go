package pokedict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
)

const (
	BOT_TOKEN    = ""
	PAGE_TOKEN   = ""
	FBMessageURI = "https://graph.facebook.com/v2.6/me/messages?access_token=" + PAGE_TOKEN

	TG_TOKEN      = ""
	TG_APIROOT    = "https://api.telegram.org/bot" + TG_TOKEN
	TG_MessageURI = TG_APIROOT + "/sendMessage"

	WELCOME_TEXT = `你好，歡迎使用 PokéDict。請輸入任何遊戲內容，機器人會為您搜尋適當的神奇寶貝資訊。`
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
	Id int64 `json:"id,string"`
}

type FBRecipient struct {
	Id int64 `json:"id,string"`
}

type FBMessage struct {
	Sender    FBSender           `json:"sender,omitempty"`
	Recipient FBRecipient        `json:"recipient,omitempty"`
	Timestamp int64              `json:"timestamp,omitempty"`
	Content   *FBMessageContent  `json:"message,omitempty"`
	Delivery  *FBMessageDelivery `json:"delivery,omitempty"`
	Postback  *FBMessagePostback `json:"postback,omitempty"`
}

type FBMessageContent struct {
	Text        string                `json:"text"`
	Seq         int64                 `json:"seq,omitempty"`
	Attachments []FBMessageAttachment `json:"attachments,omitempty"`
}

type FBMessageAttachment struct {
	Title   string
	Url     string
	Type    string
	Payload json.RawMessage
}

type FBMessageDelivery struct {
	Watermark int64 `json:"watermark"`
	Seq       int64 `json:"seq"`
}

type FBMessagePostback struct {
	Payload string `json:"payload"`
}

type TGEntry struct {
	Id      int64     `json:"update_id"`
	Message TGMessage `json:"message"`
}

type TGUser struct {
	Id        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

type TGChat struct {
	Id        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Type      string `json:"type"`
}

type TGMessage struct {
	Id        int64  `json:"message_id"`
	Timestamp int64  `json:"date"`
	From      TGUser `json:"from"`
	Chat      TGChat `json:"chat"`
	Text      string `json:"text"`
}

type User struct {
	Id                int64
	TodoAction        string
	LastText          string
	FollowedPokemonId []int64
}

var users map[int64]*User = map[int64]*User{}
var monsterList []Pokemon = []Pokemon{}
var skillList []PokemonSkill = []PokemonSkill{}

func init() {
	http.HandleFunc("/tgCallback", tgCBHandler)
	http.HandleFunc("/fbCallback", fbCBHandler)
	http.HandleFunc("/", handler)
}

func loadSkillData(ctx context.Context) {
	skillKeys := []*datastore.Key{}
	skillList = []PokemonSkill{}

	f, err := os.Open("data/fastSkill.json")
	if err != nil {
		log.Errorf(ctx, err.Error())
	}
	d := json.NewDecoder(f)

	fastSkills := []PokemonSkill{}
	err = d.Decode(&fastSkills)
	if err != nil {
		log.Errorf(ctx, err.Error())
	}
	f.Close()

	for i, skill := range fastSkills {
		skill.Id = int64(i)
		skill.Kind = "fast"
		skillKeys = append(skillKeys, datastore.NewKey(ctx, "PokemonSkill", skill.Name, 0, nil))
		skillList = append(skillList, skill)
	}

	f, err = os.Open("data/chargeSkill.json")
	if err != nil {
		log.Errorf(ctx, err.Error())
	}
	d = json.NewDecoder(f)

	chargedSkills := []PokemonSkill{}
	err = d.Decode(&chargedSkills)
	if err != nil {
		log.Errorf(ctx, err.Error())
	}
	f.Close()

	for i, skill := range chargedSkills {
		skill.Id = int64(i) + 1000
		skill.Kind = "charged"
		skillKeys = append(skillKeys, datastore.NewKey(ctx, "PokemonSkill", skill.Name, 0, nil))
		skillList = append(skillList, skill)
	}

	log.Debugf(ctx, "%+v", skillList)
	_, err = datastore.PutMulti(ctx, skillKeys, skillList)
	if err != nil {
		log.Errorf(ctx, err.Error())
	}
}

func loadMonsterData(ctx context.Context) {
	monsterKeys := []*datastore.Key{}
	monsterList = []Pokemon{}
	f, err := os.Open("data/pokemon.json")
	if err != nil {
		log.Errorf(ctx, err.Error())
		return
	}
	defer f.Close()

	d := json.NewDecoder(f)
	err = d.Decode(&monsterList)
	if err != nil {
		log.Errorf(ctx, err.Error())
		return
	}

	for _, p := range monsterList {
		monsterKeys = append(monsterKeys, datastore.NewKey(ctx, "Pokemon", p.Name, 0, nil))
	}
	log.Debugf(ctx, "%+v", monsterList)
	_, err = datastore.PutMulti(ctx, monsterKeys, monsterList)
	if err != nil {
		log.Errorf(ctx, err.Error())
		return
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Hi, this is an FB Bot for PokéDict.")
	ctx := appengine.NewContext(r)
	loadSkillData(ctx)
	loadMonsterData(ctx)
}

func tgSendTextMessage(ctx context.Context, chatId int64, text string) (err error) {
	v := url.Values{}
	v.Set("chat_id", fmt.Sprintf("%d", chatId))
	v.Set("text", text)

	url := TG_MessageURI + fmt.Sprintf("?%s", v.Encode())
	log.Debugf(ctx, "Url: %s", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}

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

func tgCBHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	var tgEntry TGEntry

	d := json.NewDecoder(r.Body)
	err := d.Decode(&tgEntry)
	if err != nil {
		log.Errorf(ctx, "%s", err.Error())
		http.Error(w, "can not parse tg entry", http.StatusInternalServerError)
	}

	text := tgEntry.Message.Text
	if text != "" {
		skills := querySkill(text)
		returnText := formatSkills(skills)

		err := tgSendTextMessage(ctx, tgEntry.Message.Chat.Id, returnText)
		if err != nil {
			log.Errorf(ctx, "%s", err.Error())
			http.Error(w, "fail to deliver a message to a client", http.StatusInternalServerError)
		}
	}

	log.Infof(ctx, "%+v", tgEntry)
	fmt.Fprint(w, "")
}

func fbSendTextMessage(ctx context.Context, senderId int64, text string) (err error) {
	payload := FBMessage{
		Recipient: FBRecipient{senderId},
		Content: &FBMessageContent{
			Text: text,
		},
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return
	}

	log.Debugf(ctx, "Payload %s", b)
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

func querySkill(skillName string) []PokemonSkill {
	foundSkills := make([]PokemonSkill, 0)
	for _, s := range skillList {
		if strings.Contains(strings.ToLower(s.Name), strings.ToLower(skillName)) {
			foundSkills = append(foundSkills, s)
		}
	}
	return foundSkills
}

func queryMonster(monsterName string) []Pokemon {
	foundMonsters := make([]Pokemon, 0)
	for _, m := range monsterList {
		if strings.Contains(strings.ToLower(m.Name), strings.ToLower(monsterName)) {
			foundMonsters = append(foundMonsters, m)
		}
	}
	return foundMonsters
}

func formatSkills(skills []PokemonSkill) string {
	if numSkill := len(skills); numSkill == 0 {
		return "什麼也沒找到"
	} else if numSkill == 1 {
		s := skills[0]
		return fmt.Sprintf("%s (%s)\nDPS: %.2f", s.Name, s.Cname, s.Dps)
	} else {
		buf := bytes.NewBuffer([]byte{})
		buf.WriteString("找到的技能如下:\n")
		for _, s := range skills {
			fmt.Fprintf(buf, "*) %s (%s)\n-> DPS: %.2f\n", s.Name, s.Cname, s.Dps)
		}
		return buf.String()
	}
}

func formatMonsters(monsters []Pokemon) string {
	if numSkill := len(monsters); numSkill == 0 {
		return "什麼也沒找到"
	} else if numSkill == 1 {
		s := monsters[0]
		typeII := ""
		if s.TypeII != "" {
			typeII = fmt.Sprintf("/%s", s.TypeII)
		}
		return fmt.Sprintf("%s (%s)\nType: %s%s", s.Name, s.Cname, s.TypeI, typeII)
	} else {
		buf := bytes.NewBuffer([]byte{})
		buf.WriteString("找到的寵物如下:\n")
		for _, s := range monsters {
			typeII := ""
			if s.TypeII != "" {
				typeII = fmt.Sprintf("/%s", s.TypeII)
			}
			fmt.Fprintf(buf, "*) %s (%s)\nType: %s%s\n", s.Name, s.Cname, s.TypeI, typeII)
		}
		return buf.String()
	}
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
	log.Debugf(ctx, "%+v", fbMessages)

	for _, fbMsg := range fbMessages {
		senderId := fbMsg.Sender.Id
		user, ok := users[senderId]
		if !ok {
			user = &User{
				Id: senderId,
			}
			users[senderId] = user
		}
		log.Debugf(ctx, "%+v", fbMsg)

		if fbMsg.Content != nil {
			var (
				err        error
				returnText string
			)
			text := fbMsg.Content.Text
			q := strings.ToLower(text)
			switch q {
			case "get started":
				fallthrough
			case "hi", "hello", "你好", "您好":
				returnText = WELCOME_TEXT
			case "找技能", "技能", "skill":
				user.TodoAction = "QUERY_MONSTER"
				returnText = "找什麼技能？"
			case "找寵物", "寵物", "pokemon", "mon":
				user.TodoAction = "QUERY_SKILL"
				returnText = "找什麼寵物？"
			default:
				switch user.TodoAction {
				case "QUERY_MONSTER":
					skills := queryMonster(text)
					returnText = formatMonsters(skills)
				case "QUERY_SKILL":
					skills := querySkill(text)
					returnText = formatSkills(skills)
				default:
					user.TodoAction = ""
					returnText = "我不懂你的意思。"
				}
			}

			err = fbSendTextMessage(ctx, senderId, returnText)
			if err != nil {
				log.Errorf(ctx, "%s", err.Error())
				http.Error(w, "fail to deliver a message to a client", http.StatusInternalServerError)
			}
			user.LastText = text
		} else if fbMsg.Delivery != nil {
		} else if fbMsg.Postback != nil {
			var returnText string
			switch fbMsg.Postback.Payload {
			case "QUERY_MONSTER":
			case "QUERY_SKILL":
			case "GET_STARTED":
				err = fbSendTextMessage(ctx, senderId, WELCOME_TEXT)
			default:
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
