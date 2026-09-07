package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestMembershipErrorResponses(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{{domain.ErrMembershipRequired, 402, "membership_required"}, {domain.ErrSVIPRequired, 403, "svip_required"}, {domain.ErrOwnerLimit, 409, "owner_limit_reached"}, {domain.ErrMembershipProduct, 409, "membership_product_conflict"}, {domain.ErrPendingMembershipOrder, 409, "membership_order_pending"}} {
		response := httptest.NewRecorder()
		writeError(response, test.err)
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.code) {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	writeGatewayDomainError(response, domain.ErrMembershipRequired)
	if response.Code != http.StatusPaymentRequired || !strings.Contains(response.Body.String(), "membership_required") {
		t.Fatal(response.Code, response.Body.String())
	}
	if err := responsesWebSocketAccessError(domain.ErrMembershipRequired); !errors.Is(err, domain.ErrMembershipRequired) || !strings.Contains(err.Error(), "membership") {
		t.Fatal(err)
	}
}
