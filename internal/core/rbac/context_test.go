package rbac

import (
	"errors"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCurrentUserIDAcceptsOnlyTrustedNonZeroUint(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		set     bool
		want    uint
		wantErr bool
	}{
		{name: "valid", value: uint(42), set: true, want: 42},
		{name: "missing", wantErr: true},
		{name: "wrong type", value: 42, set: true, wantErr: true},
		{name: "zero", value: uint(0), set: true, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(nil)
			if tc.set {
				context.Set(ContextUserID, tc.value)
			}
			got, err := CurrentUserID(context)
			if tc.wantErr {
				if !errors.Is(err, ErrCurrentUserUnavailable) {
					t.Fatalf("erreur = %v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got=%d err=%v", got, err)
			}
		})
	}
}
