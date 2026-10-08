package main

// OMDb supplies the IMDb rating and vote count for each show (by IMDb id), and
// the Rotten Tomatoes and Metacritic scores when it has them, which for TV is
// less often than for films. Free keys allow 1,000 lookups a day; scores are cached.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (env *Env) omdbBase() string {
	if env.OMDbBase != "" {
		return env.OMDbBase
	}
	return "https://www.omdbapi.com/"
}

type omdbResp struct {
	Response   string `json:"Response"`
	Error      string `json:"Error"`
	Rated      string `json:"Rated"`
	Metascore  string `json:"Metascore"`
	IMDbRating string `json:"imdbRating"`
	IMDbVotes  string `json:"imdbVotes"`
	Awards     string `json:"Awards"`
	Ratings    []struct {
		Source string `json:"Source"`
		Value  string `json:"Value"`
	} `json:"Ratings"`
}

// Scores looks up s.IMDbID on OMDb. It's a no-op without a key or id.
func (env *Env) Scores(ctx context.Context, s *Show) error {
	key := env.Config.OMDbKey
	if key == "" || s.IMDbID == "" {
		return nil
	}
	q := url.Values{"i": {s.IMDbID}, "apikey": {key}}
	req, _ := http.NewRequestWithContext(ctx, "GET", env.omdbBase()+"?"+q.Encode(), nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := env.HTTP.Do(req)
	if err != nil {
		return redactKey(err, key)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var r omdbResp
	if err := json.Unmarshal(body, &r); err != nil {
		return &APIError{"OMDb", resp.StatusCode, "OMDb answered " + resp.Status}
	}
	if r.Response != "True" {
		e := strings.ToLower(r.Error)
		switch {
		case strings.Contains(e, "limit"):
			return &APIError{"OMDb", 429, "OMDb's daily limit is used up — ratings refresh tomorrow"}
		case strings.Contains(e, "key"):
			return &APIError{"OMDb", 401, "OMDb rejected the key — check omdb_api_key"}
		}
		s.ScoresAt = env.Now()
		return nil
	}
	applyScores(s, &r)
	s.ScoresAt = env.Now()
	return nil
}

func applyScores(s *Show, r *omdbResp) {
	s.RT, s.Meta, s.IMDb = -1, -1, 0
	for _, x := range r.Ratings {
		switch x.Source {
		case "Rotten Tomatoes":
			if v, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(x.Value), "%")); err == nil {
				s.RT = v
			}
		case "Metacritic":
			if v, err := strconv.Atoi(strings.SplitN(x.Value, "/", 2)[0]); err == nil {
				s.Meta = v
			}
		case "Internet Movie Database":
			if v, err := strconv.ParseFloat(strings.SplitN(x.Value, "/", 2)[0], 64); err == nil {
				s.IMDb = v
			}
		}
	}
	if s.Meta < 0 {
		if v, err := strconv.Atoi(r.Metascore); err == nil {
			s.Meta = v
		}
	}
	if s.IMDb == 0 {
		if v, err := strconv.ParseFloat(r.IMDbRating, 64); err == nil {
			s.IMDb = v
		}
	}
	s.IMDbVotes = ""
	if r.IMDbVotes != "N/A" {
		s.IMDbVotes = r.IMDbVotes
	}
	s.Awards = ""
	if a := strings.TrimSpace(r.Awards); a != "" && a != "N/A" {
		s.Awards = a
	}
	if s.Cert == "" && r.Rated != "" && r.Rated != "N/A" {
		s.Cert = r.Rated
	}
}
