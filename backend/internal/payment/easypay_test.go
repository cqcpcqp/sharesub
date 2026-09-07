package payment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseAmount(t *testing.T) {
	for _, value := range []string{"9.9", "9.90", "009.90"} {
		amount, err := ParseAmount(value)
		if err != nil || amount != 990 {
			t.Fatalf("%q: %d %v", value, amount, err)
		}
	}
	for _, value := range []string{"", "-9.90", "+9.90", "9.900", "9.9x", "NaN", "4.99e1", "49.", ".90", " 9.90", "999999999999999999999"} {
		if _, err := ParseAmount(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestEasyPayCheckoutAndNotification(t *testing.T) {
	provider, err := New(Config{BaseURL: "https://pay.example.test/gateway/", PID: "merchant", Key: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := url.Parse(provider.CheckoutURL("20123456789012345678901234567890", "alipay", "https://share.example.test/notify", "https://share.example.test/return", 990, "VIP 会员"))
	if err != nil {
		t.Fatal(err)
	}
	values := checkout.Query()
	if checkout.Path != "/gateway/submit.php" || values.Get("money") != "9.90" || values.Get("sign") != sign(values, "secret") {
		t.Fatalf("invalid checkout %s", checkout)
	}
	values = url.Values{"pid": {"merchant"}, "out_trade_no": {"20123456789012345678901234567890"}, "trade_no": {"upstream"}, "money": {"9.90"}, "trade_status": {"TRADE_SUCCESS"}, "sign_type": {"MD5"}}
	values.Set("sign", sign(values, "secret"))
	confirmation, err := provider.Verify(values)
	if err != nil || !confirmation.Paid || confirmation.AmountCents != 990 {
		t.Fatalf("%+v %v", confirmation, err)
	}
	values.Set("money", "0.01")
	if _, err = provider.Verify(values); err == nil {
		t.Fatal("accepted tampered amount")
	}
	values.Set("money", "9.90")
	values.Add("money", "0.01")
	if _, err = provider.Verify(values); err == nil {
		t.Fatal("accepted duplicate parameters")
	}
	values.Set("money", "9.90")
	values.Set("pid", "other")
	values.Set("sign", sign(values, "secret"))
	if _, err = provider.Verify(values); err == nil {
		t.Fatal("accepted another merchant")
	}
}

func TestEasyPayQuery(t *testing.T) {
	for _, test := range []struct {
		name, body      string
		paid, wantError bool
	}{
		{"paid", `{"code":1,"pid":"merchant","out_trade_no":"order","trade_no":"trade","status":1,"money":"9.90"}`, true, false},
		{"pending", `{"code":1,"status":0}`, false, false},
		{"provider error", `{"code":0,"msg":"not found"}`, false, true},
		{"wrong order", `{"code":1,"pid":"merchant","out_trade_no":"other","trade_no":"trade","status":1,"money":"9.90"}`, false, true},
		{"wrong merchant", `{"code":1,"pid":"other","out_trade_no":"order","trade_no":"trade","status":1,"money":"9.90"}`, false, true},
		{"malformed money", `{"code":1,"pid":"merchant","out_trade_no":"order","trade_no":"trade","status":1,"money":"9.900"}`, false, true},
		{"invalid json", `<html>error</html>`, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet || request.URL.Path != "/api.php" {
					t.Errorf("unexpected query %s %s", request.Method, request.URL)
				}
				if err := request.ParseForm(); err != nil {
					t.Error(err)
				}
				if request.URL.Query().Get("out_trade_no") != "order" || request.URL.Query().Get("key") != "secret" || request.URL.Query().Get("act") != "order" || request.URL.Query().Get("pid") != "merchant" || request.ContentLength != 0 {
					t.Error("missing credentials")
				}
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			provider, err := New(Config{BaseURL: server.URL, PID: "merchant", Key: "secret"})
			if err != nil {
				t.Fatal(err)
			}
			provider.client = server.Client()
			confirmation, err := provider.Query(context.Background(), "order")
			if (err != nil) != test.wantError || confirmation.Paid != test.paid {
				t.Fatalf("%+v %v", confirmation, err)
			}
		})
	}
}

func TestEasyPayRejectsUnsafeConfiguration(t *testing.T) {
	for _, base := range []string{"http://pay.example.test", "https://user:password@pay.example.test", "https://pay.example.test?key=secret", "https://pay.example.test#fragment", ""} {
		if _, err := New(Config{BaseURL: base, PID: "merchant", Key: "secret"}); err == nil {
			t.Fatalf("accepted %q", base)
		}
	}
	provider, _ := New(Config{BaseURL: "https://pay.example.test", PID: "merchant", Key: "secret"})
	if provider.client.CheckRedirect == nil || provider.client.Timeout == 0 {
		t.Fatal("unbounded query client")
	}
	if strings.Contains(provider.CheckoutURL("order", "alipay", "notify", "return", 990, "VIP 会员"), "secret") {
		t.Fatal("merchant key exposed")
	}
}

func TestEasyPayQueryDoesNotExposeMerchantKeyOnFailure(t *testing.T) {
	provider, err := New(Config{BaseURL: "https://pay.example.test", PID: "merchant", Key: "private-merchant-key"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = provider.Query(ctx, "20123456789012345678901234567890")
	if err == nil || strings.Contains(err.Error(), "private-merchant-key") || strings.Contains(err.Error(), "api.php?") {
		t.Fatalf("query failure exposed credentials: %v", err)
	}
}
