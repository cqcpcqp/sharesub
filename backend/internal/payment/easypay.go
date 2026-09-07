package payment

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct{ BaseURL, PID, Key string }
type EasyPay struct {
	config Config
	client *http.Client
}
type Confirmation struct {
	OrderID, TradeNo, PID string
	AmountCents           int
	Paid                  bool
}

func New(config Config) (*EasyPay, error) {
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || config.PID == "" || config.Key == "" {
		return nil, errors.New("EasyPay requires an HTTPS base URL, merchant PID and key")
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")
	return &EasyPay{config: config, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func sign(values url.Values, key string) string {
	keys := make([]string, 0, len(values))
	for name := range values {
		if name != "sign" && name != "sign_type" && values.Get(name) != "" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, name := range keys {
		parts = append(parts, name+"="+values.Get(name))
	}
	digest := md5.Sum([]byte(strings.Join(parts, "&") + key))
	return hex.EncodeToString(digest[:])
}

func ParseAmount(value string) (int, error) {
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts[0]) > 9 {
		return 0, errors.New("invalid payment amount")
	}
	for _, part := range parts {
		if part == "" {
			return 0, errors.New("invalid payment amount")
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return 0, errors.New("invalid payment amount")
			}
		}
	}
	whole, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	fraction := 0
	if len(parts) == 2 {
		if len(parts[1]) > 2 {
			return 0, errors.New("invalid payment precision")
		}
		fraction, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, err
		}
		if len(parts[1]) == 1 {
			fraction *= 10
		}
	}
	return whole*100 + fraction, nil
}

func (provider *EasyPay) CheckoutURL(orderID, method, notifyURL, returnURL string, amountCents int, name string) string {
	values := url.Values{"pid": {provider.config.PID}, "type": {method}, "out_trade_no": {orderID}, "notify_url": {notifyURL}, "return_url": {returnURL}, "name": {name}, "money": {fmt.Sprintf("%d.%02d", amountCents/100, amountCents%100)}}
	values.Set("sign", sign(values, provider.config.Key))
	values.Set("sign_type", "MD5")
	return provider.config.BaseURL + "/submit.php?" + values.Encode()
}

func (provider *EasyPay) Verify(values url.Values) (Confirmation, error) {
	for _, entries := range values {
		if len(entries) != 1 {
			return Confirmation{}, errors.New("duplicate callback parameter")
		}
	}
	if values.Get("sign_type") != "MD5" || !hmac.Equal([]byte(values.Get("sign")), []byte(sign(values, provider.config.Key))) || values.Get("pid") != provider.config.PID {
		return Confirmation{}, errors.New("invalid payment signature or merchant")
	}
	amount, err := ParseAmount(values.Get("money"))
	if err != nil {
		return Confirmation{}, err
	}
	if values.Get("out_trade_no") == "" || values.Get("trade_no") == "" {
		return Confirmation{}, errors.New("missing payment identifier")
	}
	return Confirmation{OrderID: values.Get("out_trade_no"), TradeNo: values.Get("trade_no"), PID: provider.config.PID, AmountCents: amount, Paid: values.Get("trade_status") == "TRADE_SUCCESS"}, nil
}

func (provider *EasyPay) Query(ctx context.Context, orderID string) (Confirmation, error) {
	values := url.Values{"act": {"order"}, "pid": {provider.config.PID}, "key": {provider.config.Key}, "out_trade_no": {orderID}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.config.BaseURL+"/api.php?"+values.Encode(), nil)
	if err != nil {
		return Confirmation{}, err
	}
	response, err := provider.client.Do(request)
	if err != nil {
		return Confirmation{}, errors.New("payment provider query failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Confirmation{}, errors.New("payment provider query rejected")
	}
	var result struct {
		Code    int    `json:"code"`
		Status  int    `json:"status"`
		Money   string `json:"money"`
		TradeNo string `json:"trade_no"`
		OrderID string `json:"out_trade_no"`
		PID     string `json:"pid"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return Confirmation{}, errors.New("invalid payment provider response")
	}
	if result.Code != 1 {
		return Confirmation{}, errors.New("payment provider query unsuccessful")
	}
	if result.Status != 1 {
		return Confirmation{OrderID: orderID, PID: provider.config.PID}, nil
	}
	if result.OrderID != orderID || result.TradeNo == "" || result.PID != provider.config.PID {
		return Confirmation{}, errors.New("payment query identity mismatch")
	}
	amount, err := ParseAmount(result.Money)
	if err != nil {
		return Confirmation{}, err
	}
	return Confirmation{OrderID: orderID, TradeNo: result.TradeNo, PID: provider.config.PID, AmountCents: amount, Paid: true}, nil
}
