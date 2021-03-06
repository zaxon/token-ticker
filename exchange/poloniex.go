package exchange

import (
	"encoding/json"
	"errors"
	"github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// https://poloniex.com/support/api/
const poloniexBaseApi = "https://poloniex.com/"

type poloniexClient struct {
	exchangeBaseClient
	AccessKey string
	SecretKey string
}

type poloniexCommonResponse struct {
	Error *string
}

type poloniexTicker struct {
	Last          float64 `json:",string"`
	PercentChange float64 `json:",string"`
}

type poloniexKline struct {
	Date int64
	Open float64
}

func NewPoloniexClient(httpClient *http.Client) *poloniexClient {
	return &poloniexClient{exchangeBaseClient: *newExchangeBase(poloniexBaseApi, httpClient)}
}

func (client *poloniexClient) GetName() string {
	return "Poloniex"
}

func (client *poloniexClient) decodeResponse(body io.ReadCloser, result interface{}) error {
	respBytes, err := ioutil.ReadAll(body)
	defer body.Close()
	if err != nil {
		return err
	}

	var errResp struct {
		Error *string
	}
	if err := json.Unmarshal(respBytes, &errResp); err == nil && errResp.Error != nil {
		return errors.New(*errResp.Error)
	}

	return json.Unmarshal(respBytes, result)
}

func (client *poloniexClient) GetKlinePrice(symbol string, start time.Time, period int) (float64, error) {
	end := start.Add(30 * time.Minute)
	resp, err := client.httpGet("public", map[string]string{
		"command":      "returnChartData",
		"currencyPair": strings.ToUpper(symbol),
		"start":        strconv.FormatInt(start.Unix(), 10),
		"end":          strconv.FormatInt(end.Unix(), 10),
		"period":       strconv.Itoa(period),
	})
	if err != nil {
		return 0, err
	}

	var respJSON []poloniexKline
	err = client.decodeResponse(resp.Body, &respJSON)
	if err != nil {
		return 0, err
	}
	logrus.Debugf("%s - Kline for %s uses open price at %s", client.GetName(), start.Local(),
		time.Unix(respJSON[0].Date, 0).Local())
	return respJSON[0].Open, nil
}

func (client *poloniexClient) lookupSymbol(symbol string, tickers map[string]poloniexTicker) *poloniexTicker {
	symbol = strings.ToUpper(symbol)
	for name, ticker := range tickers {
		if name == symbol {
			return &ticker
		}
	}
	return nil
}

func (client *poloniexClient) GetSymbolPrice(symbol string) (*SymbolPrice, error) {
	resp, err := client.httpGet("public", map[string]string{"command": "returnTicker"})
	if err != nil {
		return nil, err
	}

	var tickers map[string]poloniexTicker
	if err := client.decodeResponse(resp.Body, &tickers); err != nil {
		return nil, err
	}
	symbolTicker := client.lookupSymbol(symbol, tickers)
	if symbolTicker == nil {
		return nil, errors.New("symbol not found")
	}

	var (
		now             = time.Now()
		percentChange1h = math.MaxFloat64
	)
	price1hAgo, err := client.GetKlinePrice(symbol, now.Add(-1*time.Hour), 300)
	if err != nil {
		logrus.Warnf("%s - Failed to get price 1 hour ago, error: %v\n", client.GetName(), err)
	} else if price1hAgo != 0 {
		percentChange1h = (symbolTicker.Last - price1hAgo) / price1hAgo * 100
	}

	return &SymbolPrice{
		Symbol:           symbol,
		Price:            strconv.FormatFloat(symbolTicker.Last, 'f', -1, 64),
		UpdateAt:         time.Now(),
		Source:           client.GetName(),
		PercentChange1h:  percentChange1h,
		PercentChange24h: symbolTicker.PercentChange * 100,
	}, nil
}

func init() {
	register((&poloniexClient{}).GetName(), func(client *http.Client) ExchangeClient {
		// Limited by type system in Go, I hate wrapper/adapter
		return NewPoloniexClient(client)
	})
}
