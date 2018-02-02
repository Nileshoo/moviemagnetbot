package movie

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/magunetto/tmdb"
)

const (
	tmdbURL = "https://www.themoviedb.org/%s/%d"
)

var (
	tapi *tmdb.TMDB

	errTMDbSearchNoResult = errors.New("No movies found on TMDb, please check your input")
)

// InitTMDb init TMDb API
func InitTMDb() {
	tapi = tmdb.New()
	if os.Getenv("TMDB_API_TOKEN") != "" {
		tapi.APIKey = os.Getenv("TMDB_API_TOKEN")
	}
}

// SearchMovies search movies on TMDb
func SearchMovies(keyword string, limit int) ([]Movie, error) {

	result, err := tapi.SearchMulti(keyword)
	if err != nil {
		log.Printf("error while querying tmdb: %s", err)
		return nil, err
	}
	if len(result.Results) == 0 {
		return nil, errTMDbSearchNoResult
	}

	return newMoviesBySearch(result, limit), nil
}

func newMoviesBySearch(result tmdb.SearchMultiResult, limit int) []Movie {

	movies := []Movie{}

	for i, r := range result.Results {
		if i == limit {
			break
		}

		m := New()
		m.TMDbID = r.ID
		m.mediaType = r.MediaType
		m.Title = r.Title
		m.Date = r.ReleaseDate
		m.TMDbURL = fmt.Sprintf(tmdbURL, r.MediaType, r.ID)
		if r.MediaType == "tv" {
			m.Title = r.Name
			m.Date = r.FirstAirDate
		}
		movies = append(movies, m)
	}

	return movies
}
