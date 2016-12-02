package morningStar

import (
	"../jsonHttp"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const IDS_URL = `https://elasticsearch.vibioh.fr/funds/morningStarId/_search?size=8000`
const PERFORMANCE_URL = `http://www.morningstar.fr/fr/funds/snapshot/snapshot.aspx?tab=1&id=`
const VOLATILITE_URL = `http://www.morningstar.fr/fr/funds/snapshot/snapshot.aspx?tab=2&id=`
const REFRESH_DELAY = 12
const CONCURRENT_FETCHER = 20

var EMPTY_BYTE = []byte(``)
var ZERO_BYTE = []byte(`0`)
var PERIOD_BYTE = []byte(`.`)
var COMMA_BYTE = []byte(`,`)
var PERCENT_BYTE = []byte(`%`)
var AMP_BYTE = []byte(`&`)
var HTML_AMP_BYTE = []byte(`&amp;`)

var LIST_REQUEST = regexp.MustCompile(`^/list$`)
var PERF_REQUEST = regexp.MustCompile(`^/(.+?)$`)

var ID = regexp.MustCompile(`"_id":"(.*?)"`)
var ISIN = regexp.MustCompile(`ISIN.:(\S+)`)
var LABEL = regexp.MustCompile(`<h1[^>]*?>((?:.|\n)*?)</h1>`)
var RATING = regexp.MustCompile(`<span\sclass=".*?stars([0-9]).*?">`)
var CATEGORY = regexp.MustCompile(`<span[^>]*?>Catégorie</span>.*?<span[^>]*?>(.*?)</span>`)
var PERF_ONE_MONTH = regexp.MustCompile(`<td[^>]*?>1 mois</td><td[^>]*?>(.*?)</td>`)
var PERF_THREE_MONTH = regexp.MustCompile(`<td[^>]*?>3 mois</td><td[^>]*?>(.*?)</td>`)
var PERF_SIX_MONTH = regexp.MustCompile(`<td[^>]*?>6 mois</td><td[^>]*?>(.*?)</td>`)
var PERF_ONE_YEAR = regexp.MustCompile(`<td[^>]*?>1 an</td><td[^>]*?>(.*?)</td>`)
var VOL_3_YEAR = regexp.MustCompile(`<td[^>]*?>Ecart-type 3 ans.?</td><td[^>]*?>(.*?)</td>`)

type SyncedMap struct {
	sync.RWMutex
	performances map[string]Performance
}

func (m *SyncedMap) get(key string) (Performance, bool) {
	m.RLock()
	defer m.RUnlock()

	performance, ok := m.performances[key]
	return performance, ok
}

func (m *SyncedMap) push(key string, performance Performance) {
	m.Lock()
	defer m.Unlock()

	m.performances[key] = performance
}

var PERFORMANCE_CACHE = SyncedMap{performances: make(map[string]Performance)}

type Performance struct {
	Id            string    `json:"id"`
	Isin          string    `json:"isin"`
	Label         string    `json:"label"`
	Category      string    `json:"category"`
	Rating        string    `json:"rating"`
	OneMonth      float64   `json:"1m"`
	ThreeMonth    float64   `json:"3m"`
	SixMonth      float64   `json:"6m"`
	OneYear       float64   `json:"1y"`
	VolThreeYears float64   `json:"v3y"`
	Score         float64   `json:"score"`
	Update        time.Time `json:"ts"`
}

type Results struct {
	Results interface{} `json:"results"`
}

func init() {
	go func() {
		ids := fetchIds()
		refreshCache(ids)

		c := time.Tick(REFRESH_DELAY * time.Hour)
		for range c {
			refreshCache(ids)
		}
	}()
}

func refreshCache(ids [][]byte) {
	log.Print(`Cache refresh - start`)
	defer log.Print(`Cache refresh - end`)
	for _, performance := range retrievePerformances(ids, fetchPerformance) {
		PERFORMANCE_CACHE.push(performance.Id, *performance)
	}
}

func readBody(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	return ioutil.ReadAll(body)
}

func getBody(url string) ([]byte, error) {
	response, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf(`Error while retrieving data from %s: %v`, url, err)
	}

	if response.StatusCode >= 400 {
		return nil, fmt.Errorf(`Got error %d while getting %s: %v`, response.StatusCode, url, err)
	}

	body, err := readBody(response.Body)
	if err != nil {
		return nil, fmt.Errorf(`Error while reading body of %s: %v`, url, err)
	}

	return body, nil
}

func extractLabel(extract *regexp.Regexp, body []byte, defaultValue []byte) []byte {
	match := extract.FindSubmatch(body)
	if match == nil {
		return defaultValue
	}

	return bytes.Replace(match[1], HTML_AMP_BYTE, AMP_BYTE, -1)
}

func extractPerformance(extract *regexp.Regexp, body []byte) float64 {
	dotResult := bytes.Replace(extractLabel(extract, body, EMPTY_BYTE), COMMA_BYTE, PERIOD_BYTE, -1)
	percentageResult := bytes.Replace(dotResult, PERCENT_BYTE, EMPTY_BYTE, -1)
	trimResult := bytes.TrimSpace(percentageResult)

	result, err := strconv.ParseFloat(string(trimResult), 64)
	if err != nil {
		return 0.0
	}
	return result
}

func cleanId(morningStarId []byte) string {
	return string(bytes.ToLower(morningStarId))
}

func fetchPerformance(morningStarId []byte) (*Performance, error) {
	cleanId := cleanId(morningStarId)
	performanceBody, err := getBody(PERFORMANCE_URL + cleanId)
	if err != nil {
		return nil, err
	}

	volatiliteBody, err := getBody(VOLATILITE_URL + cleanId)
	if err != nil {
		return nil, err
	}

	isin := string(extractLabel(ISIN, performanceBody, EMPTY_BYTE))
	label := string(extractLabel(LABEL, performanceBody, EMPTY_BYTE))
	rating := string(extractLabel(RATING, performanceBody, ZERO_BYTE))
	category := string(extractLabel(CATEGORY, performanceBody, EMPTY_BYTE))
	oneMonth := extractPerformance(PERF_ONE_MONTH, performanceBody)
	threeMonths := extractPerformance(PERF_THREE_MONTH, performanceBody)
	sixMonths := extractPerformance(PERF_SIX_MONTH, performanceBody)
	oneYear := extractPerformance(PERF_ONE_YEAR, performanceBody)
	volThreeYears := extractPerformance(VOL_3_YEAR, volatiliteBody)

	score := (0.25 * oneMonth) + (0.3 * threeMonths) + (0.25 * sixMonths) + (0.2 * oneYear) - (0.1 * volThreeYears)
	scoreTruncated := float64(int(score*100)) / 100

	return &Performance{cleanId, isin, label, category, rating, oneMonth, threeMonths, sixMonths, oneYear, volThreeYears, scoreTruncated, time.Now()}, nil
}

func fetchIds() [][]byte {
	if idsBody, err := getBody(IDS_URL); err != nil {
		log.Print(err)
		return nil
	} else {
		idsMatch := ID.FindAllSubmatch(idsBody, -1)

		ids := make([][]byte, 0, len(idsMatch))
		for _, match := range idsMatch {
			ids = append(ids, match[1])
		}

		return ids
	}
}

func retrievePerformance(morningStarId []byte) (*Performance, error) {
	cleanId := cleanId(morningStarId)

	performance, ok := PERFORMANCE_CACHE.get(cleanId)
	if ok && time.Now().Add(time.Hour*-(REFRESH_DELAY-1)).Before(performance.Update) {
		return &performance, nil
	}

	if performance, err := fetchPerformance(morningStarId); err != nil {
		return nil, err
	} else {
		PERFORMANCE_CACHE.push(cleanId, *performance)
		return performance, nil
	}
}

func concurrentRetrievePerformances(ids [][]byte, wg *sync.WaitGroup, performances chan<- *Performance, method func([]byte) (*Performance, error)) {
	tokens := make(chan int, CONCURRENT_FETCHER)

	clearSemaphores := func() {
		wg.Done()
		<-tokens
	}

	for _, id := range ids {
		tokens <- 1

		go func(morningStarId []byte) {
			defer clearSemaphores()
			if performance, err := method(morningStarId); err == nil {
				performances <- performance
			}
		}(id)
	}
}

func retrievePerformances(ids [][]byte, method func([]byte) (*Performance, error)) []*Performance {
	var wg sync.WaitGroup
	wg.Add(len(ids))

	performances := make(chan *Performance, CONCURRENT_FETCHER)
	go concurrentRetrievePerformances(ids, &wg, performances, method)

	go func() {
		wg.Wait()
		close(performances)
	}()

	results := make([]*Performance, 0, len(ids))
	for performance := range performances {
		results = append(results, performance)
	}

	return results
}

func performanceHandler(w http.ResponseWriter, morningStarId []byte) {
	performance, err := retrievePerformance(morningStarId)

	if err != nil {
		http.Error(w, err.Error(), 500)
	} else {
		jsonHttp.ResponseJson(w, *performance)
	}
}

func listHandler(w http.ResponseWriter, r *http.Request) {
	listBody, err := readBody(r.Body)
	if err != nil {
		http.Error(w, `Error while reading body for list: `+err.Error(), 500)
		return
	}

	if len(bytes.TrimSpace(listBody)) == 0 {
		jsonHttp.ResponseJson(w, Results{[0]Performance{}})
		return
	}

	jsonHttp.ResponseJson(w, Results{retrievePerformances(bytes.Split(listBody, COMMA_BYTE), retrievePerformance)})
}

type Handler struct {
}

func (handler Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add(`Access-Control-Allow-Origin`, `*`)
	w.Header().Add(`Access-Control-Allow-Headers`, `Content-Type`)
	w.Header().Add(`Access-Control-Allow-Methods`, `GET, POST`)
	w.Header().Add(`X-Content-Type-Options`, `nosniff`)

	urlPath := []byte(r.URL.Path)

	if LIST_REQUEST.Match(urlPath) {
		listHandler(w, r)
	} else if PERF_REQUEST.Match(urlPath) {
		performanceHandler(w, PERF_REQUEST.FindSubmatch(urlPath)[1])
	}
}
