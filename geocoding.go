package tankabot

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ringsaturn/tzf"
)

// YahooPlaceInfoResults は、Yahoo場所情報APIからのデータを格納する
type YahooPlaceInfoResults struct {
	ResultSet struct {
		Result []struct {
			Name     string `json:"Name"`
			Where    string `json:"Where"`
			Combined string `json:"Combined"`
		} `json:"Result"`
	} `json:"ResultSet"`
}

// YahooContentsGeoCoderResults は、YahooコンテンツジオコーダAPIからのデータを格納する
type YahooContentsGeoCoderResults struct {
	ResultInfo struct {
		Count int `json:"Count"`
	} `json:"ResultInfo"`
	Feature []struct {
		Name     string `json:"Name"`
		Geometry struct {
			Coordinates string `json:"Coordinates"`
		} `json:"Geometry"`
		Property struct {
			Address string `json:"Address"`
		} `json:"Property"`
	}
}

// SunInfo は、日の入りと日の出時刻を格納する
type SunInfo struct {
	Results struct {
		Rise string `json:"civil_twilight_begin"`
		Set  string `json:"civil_twilight_end"`
		Noon string `json:"solar_noon"`
	} `json:"results"`
}

// getLocDatafromCoordinates は、botの座標から所在地情報を取得して格納する
func getLocDataFromCoordinates(key string, lat, lng float64) (name, timeZone string, err error) {
	key = url.QueryEscape(key)
	query := "https://map.yahooapis.jp/placeinfo/V1/get?lat=" + fmt.Sprint(lat) + "&lon=" + fmt.Sprint(lng) + "&appid=" + key + "&sort=-match&output=json"

	res, err := http.Get(query)
	if err != nil {
		log.Printf("info: map.yahooapis.jpへのリクエストに失敗しました：%s", err)
		return
	}
	if code := res.StatusCode; code >= 400 {
		err = fmt.Errorf("map.yahooapis.jpへの接続エラーです(%d)", code)
		log.Printf("info: %s", err)
		return
	}
	var yr YahooPlaceInfoResults
	if err = json.NewDecoder(res.Body).Decode(&yr); err != nil {
		log.Printf("info: map.yahooapis.jpからのレスポンスがデコードできませんでした：%s", err)
		res.Body.Close()
		return
	}
	res.Body.Close()

	if yr.ResultSet.Result[0].Name == "" {
		if yr.ResultSet.Result[0].Where == "" {
			name = "地球のどこか"
		} else {
			name = yr.ResultSet.Result[0].Where
		}
	} else {
		name = yr.ResultSet.Result[0].Where + "の" + yr.ResultSet.Result[0].Name
	}

	var f tzf.F
	f, err = tzf.NewDefaultFinder()
	if err != nil {
		log.Printf("info: %s", err)
		return
	}
	timeZone = f.GetTimezoneName(lng, lat)

	return
}

// getDayCycleBySunMovement は、太陽の出入り時刻と現在時刻に応じて寝起きの時刻を返す
func getDayCycleBySunMovement(zone string, lat, lng float64) (sleep, active time.Duration, cond string, err error) {
	wt, st, err := getSleepWakeTimeBySunMovement(zone, lat, lng)
	if err != nil {
		return
	}

	if wt.IsZero() {
		sleep = time.Until(st)
		active = 0
		cond = "極夜"
		return
	}

	if st.IsZero() {
		sleep = 0
		active = time.Until(wt)
		cond = "白夜"
		return
	}

	sleep = time.Until(wt)
	tillSleep := time.Until(st)
	active = st.Sub(wt)
	if active < 0 {
		active += 24 * time.Hour
	}
	if active > tillSleep {
		sleep = 0
		active = tillSleep
	}

	return
}

// getSleepWakeTimeBySunMovement は、太陽の出入り時刻と現在時刻に応じて寝起きの時刻を返す
func getSleepWakeTimeBySunMovement(zone string, lat, lng float64) (wt, st time.Time, err error) {
	var loc *time.Location
	if strings.Contains(zone, "GMT") {
		offset, _ := strconv.Atoi(strings.Replace(zone, "GMT", "", -1))
		loc = time.FixedZone(zone, offset*60*60)
		log.Printf("info: GMTからのオフセット：%d", offset)
	} else {
		loc, err = time.LoadLocation(zone)
	}
	if err != nil {
		loc = time.Local
	}
	now := time.Now().In(loc)

	today, tomorrow := "today", "tomorrow"
	days := [...]string{today, tomorrow}

	sunrise := make(map[string]time.Time, len(days))
	sunset := make(map[string]time.Time, len(days))
	noon := make(map[string]time.Time, len(days))

	format := "2006-01-02T15:04:05-07:00"

	for i, day := range days {
		y, m, d := now.Add(time.Duration(i) * 24 * time.Hour).Date()
		dst := fmt.Sprintf("%d-%d-%d", y, int(m), d)
		url := "https://api.sunrise-sunset.org/json?" + "lat=" + fmt.Sprint(lat) + "&lng=" + fmt.Sprint(lng) + "&date=" + dst + "&formatted=0"

		res, err := http.Get(url)
		if err != nil {
			log.Printf("info: 日の出日没時刻サイトへのリクエストに失敗しました：%s", err)
			return wt, st, err
		}
		if code := res.StatusCode; code >= 400 {
			err = fmt.Errorf("info 日の出日没時刻サイトへの接続エラーです(%d)", code)
			log.Printf("info: %s", err)
			return wt, st, err
		}
		var sun SunInfo
		if err = json.NewDecoder(res.Body).Decode(&sun); err != nil {
			log.Printf("info: %s の太陽の出入り時刻がデコードできませんでした：%s", day, err)
			res.Body.Close()
			return wt, st, err
		}
		res.Body.Close()

		sunrise[day], _ = time.Parse(format, sun.Results.Rise)
		sunset[day], _ = time.Parse(format, sun.Results.Set)
		noon[day], _ = time.Parse(format, sun.Results.Noon)

	}

	for _, day := range days {
		y, _, _ := sunrise[day].Date()
		if y == 1970 {
			wt, st = getExtremeCycle(noon, loc, lat)
			return wt, st, nil
		}
	}

	wt = sunrise[today]
	st = sunset[today]

	now = time.Now()
	if sunrise[today].Before(now) {
		wt = sunrise[tomorrow]
	}
	if sunset[today].Before(now) {
		st = sunset[tomorrow]
	}

	return
}

// getExtremeCycle は、白夜あるいは極夜の生活サイクルを返す
func getExtremeCycle(noon map[string]time.Time, loc *time.Location, lat float64) (wt, st time.Time) {
	now := time.Now()
	today, tomorrow := "today", "tomorrow"

	isWhite := whiteOrBlack(loc, lat)

	if isWhite {
		// wtを１日で最も暗い時刻に設定
		wt = noon[today].Add(12 * time.Hour)
		if wt.Before(now) {
			wt = noon[tomorrow].Add(12 * time.Hour)
		}
		st = time.Time{}
	} else {
		// stを１日で最も明るい時刻に設定
		wt = time.Time{}
		st = noon[today]
		if st.Before(now) {
			st = noon[tomorrow]
		}
	}

	return
}

// whiteOrBlack は、現在その地点が白夜か極夜かを返す
func whiteOrBlack(loc *time.Location, lat float64) bool {
	now := time.Now().In(loc)
	_, m, _ := now.Date()

	if lat > 0 {
		if 3 < int(m) && int(m) < 10 {
			// 白夜
			return true
		}
		// 極夜
		return false
	}

	if 3 < int(m) && int(m) < 10 {
		// 極夜
		return false
	}
	// 白夜
	return true
}
