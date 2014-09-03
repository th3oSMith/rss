package rss

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

type Credentials struct {
	Username string
	Password string
}

// Parse RSS or Atom data.
func Parse(data []byte) (*Feed, error) {

	if strings.Contains(string(data), "<rss") {
		return parseRSS2(data, database)
	} else if strings.Contains(string(data), "xmlns=\"http://purl.org/rss/1.0/\"") {
		return parseRSS1(data, database)
	} else {
		return parseAtom(data, database)
	}

	panic("Unreachable.")
}

// CacheParsedItemIDs enables or disable Item.ID caching when parsing feeds.
// Returns whether Item.ID were cached prior to function call.
func CacheParsedItemIDs(flag bool) (didCache bool) {
	didCache = !disabled
	disabled = !flag
	return
}

type FetchFunc func() (resp *http.Response, err error)

// Fetch downloads and parses the RSS feed at the given URL
func Fetch(url string, insecure bool, credentials Credentials) (*Feed, error) {
	if insecure {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}

		return FetchByClient(url, &http.Client{Transport: tr}, insecure, credentials)
	}
	return FetchByClient(url, http.DefaultClient, insecure, credentials)
}

func FetchByClient(url string, client *http.Client, insecure bool, credentials Credentials) (*Feed, error) {
	fetchFunc := func() (resp *http.Response, err error) {

		if credentials.Username != "" && credentials.Password != "" {
			request, _ := http.NewRequest("GET", url, nil)
			request.SetBasicAuth(credentials.Username, credentials.Password)

			return client.Do(request)
		}

		return client.Get(url)
	}
	return FetchByFunc(fetchFunc, url, insecure, credentials)
}

func FetchByFunc(fetchFunc FetchFunc, url string, insecure bool, credentials Credentials) (*Feed, error) {
	resp, err := fetchFunc()
	if err != nil {
		if strings.Contains(err.Error(), "unknown authority") {
			return nil, errors.New("Certificat signed by unknown authority")
		}
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	out, err := Parse(body)
	if err != nil {
		if credentials.Username != "" && strings.Contains(err.Error(), "<feed>") {
			return nil, errors.New("Wrong Login/Password")
		}
		return nil, err
	}

	if out.Link == "" {
		out.Link = url
	}

	out.UpdateURL = url
	out.Insecure = insecure
	out.Credentials = credentials

	return out, nil
}

// Feed is the top-level structure.
type Feed struct {
	Nickname    string
	Title       string `json:"title"`
	Description string `json:"description"`
	Link        string `json:"link"`
	UpdateURL   string `json:"xmlurl"`
	Image       *Image
	Items       []*Item
	ItemMap     map[string]struct{}
	Refresh     time.Time
	UpdateDate  time.Time `json:"updateDate"`
	Unread      uint32
	Id          int64  `json:"id"`
	Status      string `json:"status"`
	Insecure    bool
	Credentials Credentials
}

// Update fetches any new items and updates f.
func (f *Feed) Update() error {

	// Check that we don't update too often.
	if f.Refresh.After(time.Now()) {
		return nil
	}

	if f.UpdateURL == "" {
		return errors.New("Error: feed has no URL.")
	}

	if f.ItemMap == nil {
		f.ItemMap = make(map[string]struct{})
		for _, item := range f.Items {
			if _, ok := f.ItemMap[item.ID]; !ok {
				f.ItemMap[item.ID] = struct{}{}
			}
		}
	}

	update, err := Fetch(f.UpdateURL, f.Insecure, f.Credentials)
	if err != nil {
		return err
	}

	f.Refresh = update.Refresh
	f.Title = update.Title
	f.Description = update.Description
	f.UpdateDate = time.Now()

	for _, item := range update.Items {
		if _, ok := f.ItemMap[item.ID]; !ok {
			f.Items = append(f.Items, item)
			f.ItemMap[item.ID] = struct{}{}
			f.Unread++
		}
	}

	return nil
}

/**
 * Récupération des nouveaux Articles d'un Flux
 */
func (f *Feed) GetNew() (articles []*Item, err error) {

	/*// On ne vérifie pas trop souvent
	if f.Refresh.After(time.Now()) {
		return nil, nil
	}
	*/

	if f.UpdateURL == "" {
		return nil, errors.New("Error: Le flux n'a pas d'URL")
	}

	update, err := Fetch(f.UpdateURL, f.Insecure, f.Credentials)
	if err != nil {
		if strings.Contains(err.Error(), "unknown authority") {
			f.Status = "error: Certificat signed by unknown authority"
			return nil, err
		}
		if f.Credentials.Username != "" && strings.Contains(err.Error(), "Wrong") {
			f.Status = "error: Wrong Login/Password"
			return nil, err
		}
		f.Status = "error: Impossible de récupérer le flux"
		return nil, err
	}

	f.Refresh = update.Refresh
	f.Title = update.Title
	f.Description = update.Description
	f.UpdateDate = time.Now()
	f.Status = "Updated"

	for _, item := range update.Items {
		// Si l'article n'a pas de date de publication on prend la date de récupération
		if time.Since(item.Date).Hours() > 24 {
			item.Date = time.Now()
		}
		articles = append(articles, item)
		f.Unread++
	}
	update = &Feed{}

	if len(articles) == 0 {
		f.Status = "not modified"
	}

	return articles, nil

}

func (f *Feed) String() string {
	buf := new(bytes.Buffer)
	buf.WriteString(fmt.Sprintf("Feed %q\n\t%q\n\t%q\n\t%s\n\tRefresh at %s\n\tUnread: %d\n\tItems:\n",
		f.Title, f.Description, f.Link, f.Image, f.Refresh.Format("Mon 2 Jan 2006 15:04:05 MST"), f.Unread))
	for _, item := range f.Items {
		buf.WriteString(fmt.Sprintf("\t%s\n", item.Format("\t\t")))
	}
	return buf.String()
}

// Item represents a single story.
type Item struct {
	Title   string    `json:"title"`
	Content string    `json:"description"`
	Link    string    `json:"link"`
	Date    time.Time `json:"date"`
	PubDate time.Time `json:"pubdate"`
	ID      string
	Read    bool
	Id      int64  `json:"id"`
	Feed    string `json:"feed"`
}

func (i *Item) String() string {
	return i.Format("")
}

func (i *Item) Format(s string) string {
	return fmt.Sprintf("Item %q\n\t%s%q\n\t%s%s\n\t%s%q\n\t%sRead: %v\n\t%s%q", i.Title, s, i.Link, s,
		i.Date.Format("Mon 2 Jan 2006 15:04:05 MST"), s, i.ID, s, i.Read, s, i.Content)
}

type Image struct {
	Title  string
	Url    string
	Height uint32
	Width  uint32
}

func (i *Image) String() string {
	return fmt.Sprintf("Image %q", i.Title)
}

func Restore(known map[string]struct{}) {
	database.setKnown(known)
}

func GetState() (known map[string]struct{}) {
	return database.getKnown()
}
