package pokedict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TomiHiltunen/geohash-golang"
	goradar "github.com/lemonlatte/goradar-api/api"

	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/memcache"
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

var lock sync.Mutex = sync.Mutex{}

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

type FBMessageQuickReply struct {
	Payload string
}

type FBMessageContent struct {
	Text        string                `json:"text"`
	Seq         int64                 `json:"seq,omitempty"`
	Attachments []FBMessageAttachment `json:"attachments,omitempty"`
	QuickReplay *FBMessageQuickReply  `json:"quick_reply,omitempty"`
}

type FBMessageAttachment struct {
	Title   string          `json:",omitempty"`
	Url     string          `json:",omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type FBLocationAttachment struct {
	Coordinates Location `json:"coordinates"`
}

type Location struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"long"`
}

type FBMessageTemplate struct {
	Type     string          `json:"template_type"`
	Elements json.RawMessage `json:"elements"`
}

type FBButtonItem struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Url     string `json:"url,omitempty"`
	Payload string `json:"payload,omitempty"`
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
var monsterMap map[int64]Pokemon = map[int64]Pokemon{}
var skillMap map[int64]PokemonSkill = map[int64]PokemonSkill{}

func init() {
	http.HandleFunc("/tgCallback", tgCBHandler)
	http.HandleFunc("/fbCallback", fbCBHandler)
	http.HandleFunc("/", handler)
}

func loadSkillData(ctx context.Context) {
	lock.Lock()
	defer lock.Unlock()

	if len(skillMap) != 0 {
		return
	}

	skillKeys := []*datastore.Key{}
	skillList := []PokemonSkill{}

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
		skillMap[skill.Id] = skill
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
		skillMap[skill.Id] = skill
	}

	log.Debugf(ctx, "%+v", skillList)
	_, err = datastore.PutMulti(ctx, skillKeys, skillList)
	if err != nil {
		log.Errorf(ctx, err.Error())
	}
}

func loadMonsterData(ctx context.Context) {
	lock.Lock()
	defer lock.Unlock()

	if len(monsterMap) != 0 {
		return
	}

	monsterKeys := []*datastore.Key{}
	monsterList := []Pokemon{}
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
		monsterMap[p.Id] = p
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
		skills := querySkill(ctx, []string{text})
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

func fbSendTextMessage(ctx context.Context, senderId int64, text string, quickReplies []map[string]string) (err error) {
	var message map[string]interface{}
	if quickReplies != nil {
		message = map[string]interface{}{
			"text":          text,
			"quick_replies": quickReplies,
		}
	} else {
		message = map[string]interface{}{"text": text}
	}

	payload := map[string]interface{}{
		"recipient": FBRecipient{senderId},
		"message":   message,
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

func fbSendGeneralTemplate(ctx context.Context, senderId int64, elements json.RawMessage) (err error) {
	msgPayload := FBMessageTemplate{
		Type:     "generic",
		Elements: elements,
	}

	msgBuf, err := json.Marshal(&msgPayload)
	if err != nil {
		return
	}

	payload := map[string]interface{}{
		"recipient": FBRecipient{senderId},
		"message": map[string]interface{}{
			"attachment": &FBMessageAttachment{
				Type:    "template",
				Payload: json.RawMessage(msgBuf),
			},
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

func querySkill(ctx context.Context, skillNames []string) (foundSkills []PokemonSkill) {
	if len(skillMap) == 0 {
		loadSkillData(ctx)
	}

	foundSkills = make([]PokemonSkill, 0)
	skillToQuery := ""
	if l := len(skillNames); l > 1 {
		skillToQuery = strings.Join(skillNames, ",")
		for _, s := range skillMap {
			if strings.Contains(strings.ToLower(skillToQuery), strings.ToLower(s.Name)) {
				foundSkills = append(foundSkills, s)
			}
		}
	} else if l == 1 {
		skillToQuery = skillNames[0]
		for _, s := range skillMap {
			if strings.Contains(strings.ToLower(s.Name), strings.ToLower(skillToQuery)) {
				foundSkills = append(foundSkills, s)
			}
		}
	}
	return
}

func queryMonster(ctx context.Context, monsterName string) []Pokemon {
	if len(monsterMap) == 0 {
		loadMonsterData(ctx)
	}

	foundMonsters := make([]Pokemon, 0)
	for _, m := range monsterMap {
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

func getShortAddr(ctx context.Context, id string, latitude, longitude float64) (shortAddr string) {
	tr := &urlfetch.Transport{Context: ctx}

	if item, err := memcache.Get(ctx, id); err == memcache.ErrCacheMiss {
		r, err := getAddress(tr.RoundTrip, latitude, longitude)
		defer time.Sleep(500 * time.Millisecond)
		if err != nil {
			log.Errorf(ctx, err.Error())
		}
		log.Infof(ctx, "Address: %+v", r)
		item := &memcache.Item{
			Key:   id,
			Value: []byte(fmt.Sprintf("%s%s,%s", r.Address.State, r.Address.Suburb, r.Address.Road)),
		}
		err = memcache.Add(ctx, item)
		if err != nil {
			log.Errorf(ctx, err.Error())
		} else {
			shortAddr = string(item.Value)
		}
	} else if err != nil {
		log.Errorf(ctx, "error getting item: %v", err)
	} else {
		shortAddr = string(item.Value)
	}
	return
}

func getDistances(lat1, long1, lat2, long2 float64) float64 {
	return math.Sqrt(math.Pow((lat2-lat1)*110, 2) + math.Pow((long2-long1)*110, 2))
}

func getPokemonNear(ctx context.Context, lat, long float64, distance int64) (monsters []PokemonPin, err error) {
	if len(monsterMap) == 0 {
		loadMonsterData(ctx)
	}

	tr := &urlfetch.Transport{Context: ctx}
	data, err := goradar.GetPokemon(tr.RoundTrip, lat, long, distance)
	if err != nil {
		log.Errorf(ctx, "%+v", err)
		return
	}

	monsters = []PokemonPin{}
	for _, pl := range data.Pokemons {
		switch pl.PokemonId {
		case 3, 6, 9, 26, 28, 31, 34, 38, 45, 51, 57, 58, 59, 62, 65, 68, 71, 76, 78, 82,
			89, 94, 97, 103, 106, 107, 108, 113, 115, 122, 128, 130, 131, 132, 134, 135,
			136, 142, 143, 144, 145, 146, 147, 148, 149, 150, 151:
			pp := PokemonPin{
				Id:            pl.Id,
				Pokemon:       monsterMap[pl.PokemonId],
				DisappearTime: pl.DisappearTime,
				Distance:      pl.Distance,
				Latitude:      pl.Location.Latitude,
				Longitude:     pl.Location.Longitude,
				Geohash:       geohash.EncodeWithPrecision(pl.Location.Latitude, pl.Location.Longitude, 6),
			}
			monsters = append(monsters, pp)
		}
	}
	return
}

func generateTemplateElements(ctx context.Context, items []map[string]interface{}) (elements []map[string]interface{}) {
	elements = []map[string]interface{}{}

	for _, item := range items {
		element := map[string]interface{}{
			"title":     item["title"],
			"image_url": item["image_url"],
			"item_url":  item["item_url"],
			"subtitle":  item["subtitle"],
			"buttons":   item["buttons"],
		}
		log.Debugf(ctx, "%+v", element)
		elements = append(elements, element)
	}
	return
}

func getMonsterPinElements(ctx context.Context, monsterPins []PokemonPin) []map[string]interface{} {
	results := []map[string]interface{}{}
	for _, m := range monsterPins {
		monster := m.Pokemon
		shortAddr := getShortAddr(ctx, m.Id, m.Latitude, m.Longitude)

		disappearTime := time.Unix(m.DisappearTime/1000, 0).Round(time.Second)
		loc, _ := time.LoadLocation("Asia/Taipei")
		restTime := disappearTime.Sub(time.Now().Round(time.Second))
		element := map[string]interface{}{
			"title":     fmt.Sprintf("%s (%s)", monster.Cname, monster.Name),
			"image_url": fmt.Sprintf("http://pgwave.com/assets/images/pokemon/3d-h120/%d.png", m.Pokemon.Id),
			"item_url":  fmt.Sprintf("http://maps.apple.com/maps?q=%f,%f&z=16", m.Latitude, m.Longitude),
			"subtitle":  fmt.Sprintf("位置: %s\n直線距離 %0.2fkm\n消失時間 %s (剩餘 %s)", shortAddr, m.Distance, disappearTime.In(loc).Format("15:04:05"), restTime.String()),
			"buttons": []FBButtonItem{
				FBButtonItem{
					Type:  "web_url",
					Title: "Google Map",
					Url:   fmt.Sprintf("https://maps.google.com.tw/?q=%f,%f", m.Latitude, m.Longitude),
				},
			},
		}
		log.Debugf(ctx, "%+v", element)
		results = append(results, element)
	}
	return results
}

func fbMonsterPinResponse(ctx context.Context, senderId int64, lat, long float64) (returnText string, err error) {
	monsterPins, err := getPokemonNear(ctx, lat, long, 5)
	if err != nil {
		returnText = "查詢失敗"
	} else {
		if len(monsterPins) == 0 {
			returnText = "附近沒有稀有怪"
		} else {
			log.Debugf(ctx, "%+v", monsterPins)
			if len(monsterPins) > 10 {
				monsterPins = monsterPins[0:10]
			}

			elements := getMonsterPinElements(ctx, monsterPins)
			log.Debugf(ctx, "%+v", elements)

			b, err := json.Marshal(elements)
			if err != nil {
				returnText = "查詢失敗"
			} else {
				if err := fbSendGeneralTemplate(ctx, senderId, json.RawMessage(b)); err != nil {
					returnText = "查詢失敗"
				}
			}
		}
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

		var (
			err        error
			returnText string
		)

		if fbMsg.Content != nil {
			// 如果收到 Location
			attachments := fbMsg.Content.Attachments
			if len(attachments) != 0 && attachments[0].Type == "location" {
				payload := FBLocationAttachment{}
				err = json.Unmarshal(attachments[0].Payload, &payload)
				if err != nil {
					log.Errorf(ctx, err.Error())
					return
				}
				lat := payload.Coordinates.Latitude
				long := payload.Coordinates.Longitude

				if user.TodoAction == "FIND_MONSTER" {
					returnText, err = fbMonsterPinResponse(ctx, senderId, lat, long)
					if err != nil {
						log.Errorf(ctx, "FB Pin response error: %s", err)
					}
				} else {
					text := "找怪嗎?"
					quickReplies := []map[string]string{
						map[string]string{
							"content_type": "text",
							"title":        "是",
							"payload":      fmt.Sprintf("FIND_MONSTER:%f,%f", lat, long),
						},
						map[string]string{
							"content_type": "text",
							"title":        "不是",
							"payload":      "KIDDING",
						},
					}
					err = fbSendTextMessage(ctx, senderId, text, quickReplies)
				}
			} else if fbMsg.Content.QuickReplay != nil {
				payload := fbMsg.Content.QuickReplay.Payload
				payloadItems := strings.Split(payload, ":")
				if len(payloadItems) != 0 {
					switch payloadItems[0] {
					case "FIND_MONSTER":
						user.TodoAction = "FIND_MONSTER"
						latlng := strings.Split(payloadItems[1], ",")
						if len(latlng) != 2 {
							log.Errorf(ctx, "FIND_MONSTER postback arguments error: %+v", latlng)
							returnText = "查詢錯誤"
						} else {
							lat, err := strconv.ParseFloat(latlng[0], 64)
							if err != nil {
								return
							}
							lng, err := strconv.ParseFloat(latlng[1], 64)
							if err != nil {
								return
							}
							returnText, err = fbMonsterPinResponse(ctx, senderId, lat, lng)
							log.Debugf(ctx, "return text: %s", returnText)
						}
					case "KIDDING":
						err = fbSendTextMessage(ctx, senderId, "你在呼嚨我嗎？", nil)
					}
				}
			} else {
				text := fbMsg.Content.Text
				q := strings.ToLower(text)
				switch q {
				case "get started":
					fallthrough
				case "hi", "hello", "你好", "您好":
					user.TodoAction = ""
					returnText = WELCOME_TEXT
				case "查技", "查技能", "技能", "skill":
					user.TodoAction = "QUERY_SKILL"
					returnText = "想要找什麼技能？(請輸入技能「英文」關鍵字)"
				case "查寵", "查寵物", "寵物", "pokemon", "mon":
					user.TodoAction = "QUERY_MONSTER"
					returnText = "想要找什麼寵物？(請輸入寵物「英文」關鍵字)"
				case "搜怪", "找怪", "找稀有怪":
					user.TodoAction = "FIND_MONSTER"
					returnText = "你在哪？？把你的現在位置傳 (Pin📍) 給我吧！"
				default:
					switch user.TodoAction {
					case "QUERY_MONSTER":
						monsters := queryMonster(ctx, text)
						if l := len(monsters); l == 0 {
							returnText = "沒有找到任何寵物"
						} else if l > 6 {
							returnText = "範圍太大，多打些字吧"
						} else {
							monstersItems := []map[string]interface{}{}
							for _, m := range monsters {
								typeII := ""
								if m.TypeII != "" {
									typeII = fmt.Sprintf(" / %s", m.TypeII)
								}
								monstersItems = append(monstersItems, map[string]interface{}{
									"title": fmt.Sprintf("%s (%s)", m.Cname, m.Name),
									"subtitle": fmt.Sprintf("屬性: %s%s\n最大CP: %d\n速技: %s\n充能技: %s",
										m.TypeI, typeII, m.MaxCP,
										strings.Join(m.FastMoves, ", "), strings.Join(m.ChargedMoves, ", ")),
									"image_url": fmt.Sprintf("http://pgwave.com/assets/images/pokemon/3d-h120/%d.png", m.Id),
									"item_url":  fmt.Sprintf("http://pgwave.com/zh-hant/pokemon/%s", m.Id),
									"buttons": []FBButtonItem{
										FBButtonItem{
											Type:    "postback",
											Title:   "顯示技能資訊",
											Payload: fmt.Sprintf("QUERY_MONSTER_SKILL:%d", m.Id),
										},
									},
								})
							}
							elements := generateTemplateElements(ctx, monstersItems)
							b, err := json.Marshal(elements)
							if err != nil {
								returnText = "查詢失敗"
							} else {
								if err := fbSendGeneralTemplate(ctx, senderId, json.RawMessage(b)); err != nil {
									returnText = "查詢失敗"
								}
							}
						}
					case "QUERY_SKILL":
						skills := querySkill(ctx, []string{text})
						if len(skills) > 6 {
							returnText = "範圍太大，多打些字吧"
						} else {
							returnText = formatSkills(skills)
						}
					default:
						user.TodoAction = ""
						returnText = "我不懂你的意思。"
					}
				}
				if err != nil {
					log.Errorf(ctx, "%s", err.Error())
					http.Error(w, "fail to deliver a message to a client", http.StatusInternalServerError)
				}
				user.LastText = text
			}
		} else if fbMsg.Delivery != nil {
		} else if fbMsg.Postback != nil {
			payload := fbMsg.Postback.Payload
			payloadItems := strings.Split(payload, ":")
			if len(payloadItems) != 0 {
				action := payloadItems[0]
				switch action {
				case "QUERY_MONSTER":
					returnText = "想要找什麼寵物？(請輸入寵物「英文」關鍵字)"
					user.TodoAction = fbMsg.Postback.Payload
				case "QUERY_MONSTER_SKILL":
					if len(payloadItems) == 2 {
						mId, err := strconv.ParseInt(payloadItems[1], 10, 64)
						if err == nil {
							monster := monsterMap[mId]
							err = fbSendTextMessage(ctx, senderId, fmt.Sprintf("查詢「%s」技能", monster.Cname), nil)
							skills := querySkill(ctx, append(monster.FastMoves, monster.ChargedMoves...))
							returnText = formatSkills(skills)
						} else {
							log.Errorf(ctx, "Can not parse int: %s. Error: %s", payloadItems[1], err.Error())
							returnText = "查詢過程發生錯誤"
						}
					}
				case "QUERY_SKILL":
					returnText = "想要找什麼技能？(請輸入技能「英文」關鍵字)"
					user.TodoAction = fbMsg.Postback.Payload
				case "FIND_MONSTER":
					returnText = "你在哪？？把你的現在位置傳 (Pin📍) 給我吧！"
					user.TodoAction = fbMsg.Postback.Payload
				case "GET_STARTED":
					err = fbSendTextMessage(ctx, senderId, WELCOME_TEXT, nil)
					fallthrough
				default:
					user.TodoAction = ""
				}
			}
		}
		if returnText != "" {
			err = fbSendTextMessage(ctx, senderId, returnText, nil)
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
