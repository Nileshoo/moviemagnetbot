package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/magunetto/moviemagnetbot/movie"

	"gopkg.in/tucnak/telebot.v2"
)

const (
	userFeedTitle = "Movie Magnet Bot feed"
	userFeedURL   = "https://moviemagnetbot.herokuapp.com/tasks/%s.xml"

	replyHelp       = "What movies do you like? Try me with the title, or just send the IMDb / Douban links"
	replyRarbgErr   = "We encountered an error while finding magnet links, please try again"
	replyTMDbErr    = "We encountered an error while finding movies, please try again"
	replyNoIMDbIDs  = "We encountered an error while finding IMDb IDs for you: "
	replyNoTorrents = "We have no magnet links for this movie now, please come back later"
	replyNoPubStamp = "We could not find this magnet link, please check your input"
	replyNoTMDb     = "We could not find this movie on TMDb, please check your input"
	replyNoTorrent  = "We encountered an error while finding this magnet link"
	replyFeedTips   = "Auto-download every link you requested by subscribing " + userFeedURL
	replyTaskAdded  = "Task added to your feed, it will start soon"

	cmdPrefixDown = "/dl"
	cmdPrefixTMDb = "/tmdb"

	itemsPerMovieSearch = 5
	itemsPerFeed        = 20
	feedCheckThreshold  = time.Duration(24 * time.Hour)
)

// RunBot init bot, register handlers, and start the bot
func RunBot() {

	// init bot
	b, err := telebot.NewBot(telebot.Settings{
		Token:  os.Getenv("MOVIE_MAGNET_BOT_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatalf("error while creating telebot: %s", err)
	}

	// bot handlers
	b.Handle("/start", func(m *telebot.Message) {
		b.Send(m.Sender, replyHelp)
	})
	b.Handle("/help", func(m *telebot.Message) {
		b.Send(m.Sender, replyHelp)
	})
	b.Handle(telebot.OnText, func(m *telebot.Message) {
		log.Printf("@%s: %s", m.Sender.Username, m.Text)

		// download requst
		if strings.HasPrefix(m.Text, cmdPrefixDown) {
			downloadHandler(b, m)
			return
		}
		// tmdb search
		if strings.HasPrefix(m.Text, cmdPrefixTMDb) {
			tmdbHandler(b, m)
			return
		}

		// search request
		searchHandler(b, m)
	})

	// bot loop
	b.Start()
}

func downloadHandler(b *telebot.Bot, m *telebot.Message) {

	// get `PubStamp` from command, e.g. /dl1514983115
	pubStampString := m.Text[len(cmdPrefixDown):len(m.Text)]
	pubStamp, err := strconv.Atoi(pubStampString)
	if err != nil {
		log.Printf("error while parsing timestamp: %s", err)
		b.Send(m.Sender, replyNoPubStamp)
		return
	}

	// get torrent by `PubStamp`
	t := &Torrent{PubStamp: int64(pubStamp)}
	t, err = t.getByPubStamp()
	if err != nil {
		log.Printf("error while getting torrent: %s", err)
		b.Send(m.Sender, replyNoTorrent)
		return
	}
	magnet := &t.Magnet
	b.Send(m.Sender, "`"+*magnet+"`", &telebot.SendOptions{ParseMode: telebot.ModeMarkdown})

	// save the torrent for user
	u := &User{
		TelegramID:   m.Sender.ID,
		TelegramName: m.Sender.Username,
	}
	err = u.appendTorrent(t)
	if err != nil {
		log.Printf("error while adding torrent for user: %s", err)
		return
	}

	if u.isFeedActive() {
		b.Send(m.Sender, replyTaskAdded)
		return
	}
	b.Send(m.Sender, fmt.Sprintf(replyFeedTips, u.FeedID))
}

func tmdbHandler(b *telebot.Bot, m *telebot.Message) {
	tmdbID := m.Text[len(cmdPrefixTMDb):len(m.Text)]
	buffer := new(bytes.Buffer)
	fmt.Fprintf(buffer, "§ %s\n", m.Text)
	searchTorrents(buffer, "tmdb", tmdbID)
	b.Send(m.Sender, buffer.String(), &telebot.SendOptions{ParseMode: telebot.ModeMarkdown})
}

func searchHandler(b *telebot.Bot, m *telebot.Message) {
	imdbIDs, err := searchIMDbIDsFromMessage(m.Text)
	if err != nil {
		b.Send(m.Sender, replyNoIMDbIDs+err.Error())
		return
	}

	// No IMDb found in message, take it as keyword to search movies or TVs
	if len(imdbIDs) == 0 {
		movieSearchHandler(b, m)
		return
	}

	// Found IMDbs, search torrents for each of them
	for _, id := range imdbIDs {
		torrentSearchHandler(b, m, id)
	}
}

func searchIMDbIDsFromMessage(text string) ([]string, error) {
	imdbIDs := []string{}
	// Douban
	movieLinks := findDoubanMovieURLs(text)
	for _, url := range movieLinks {
		movie := movie.New()
		if err := movie.FetchFromURL(url); err != nil {
			return nil, err
		}
		imdbIDs = append(imdbIDs, movie.IMDbID())
	}
	// IMDb
	imdbIDs = append(imdbIDs, findIMDbIDs(text)...)
	return imdbIDs, nil
}

var (
	reDoubanMovieURL = regexp.MustCompile(`http(s)?\:\/\/movie\.douban\.com\/subject\/[0-9]+`)
	reIMDbID         = regexp.MustCompile(`tt[0-9]{7}`) // e.g. tt0137523
)

func findDoubanMovieURLs(s string) []string {
	return reDoubanMovieURL.FindAllString(s, -1)
}

func findIMDbIDs(s string) []string {
	return reIMDbID.FindAllString(s, -1)
}

func movieSearchHandler(b *telebot.Bot, m *telebot.Message) {
	buf := new(bytes.Buffer)
	isSingleResult := renderMovieSearchResult(buf, m.Text)
	b.Send(m.Sender, buf.String(),
		&telebot.SendOptions{ParseMode: telebot.ModeMarkdown, DisableWebPagePreview: !isSingleResult})
}

func renderMovieSearchResult(w io.Writer, keyword string) bool {
	fmt.Fprintf(w, "§ %s\n", keyword)
	movies, err := movie.SearchMovies(keyword, itemsPerMovieSearch)
	if err != nil {
		fmt.Fprintln(w, err)
		return false
	}
	renderMovies(w, movies)
	return len(movies) == 1
}

func renderMovies(w io.Writer, movies []movie.Movie) {
	for _, m := range movies {
		command := fmt.Sprintf("%s%d", cmdPrefixTMDb, m.TMDbID)
		fmt.Fprintf(w, "%s (%s)\n", m.Title, m.Date[0:4])
		fmt.Fprintf(w, "▸ %s [¶](%s)\n", command, m.TMDbURL)
	}
}

func torrentSearchHandler(b *telebot.Bot, m *telebot.Message, id string) {
	result := new(bytes.Buffer)
	fmt.Fprintf(result, "§ /%s\n", id)
	isSingleResult := searchTorrents(result, "imdb", id)
	b.Send(m.Sender, result.String(),
		&telebot.SendOptions{ParseMode: telebot.ModeMarkdown, DisableWebPagePreview: !isSingleResult})
}
