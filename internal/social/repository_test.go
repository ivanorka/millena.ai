package social

import (
	"errors"
	"testing"
)

func TestParseSocialChannelLimit(t *testing.T) {
	value := func(input string) *string { return &input }
	tests := []struct {
		name          string
		value         *string
		wantLimit     int
		wantUnlimited bool
		wantErr       error
	}{
		{name: "all", value: value("all"), wantUnlimited: true},
		{name: "all normalized", value: value(" ALL "), wantUnlimited: true},
		{name: "numeric", value: value("3"), wantLimit: 3},
		{name: "zero", value: value("0")},
		{name: "missing", wantErr: ErrSocialChannelsUnavailable},
		{name: "negative", value: value("-1"), wantErr: ErrSocialChannelsUnavailable},
		{name: "fractional", value: value("1.5"), wantErr: ErrSocialChannelsUnavailable},
		{name: "boolean", value: value("true"), wantErr: ErrSocialChannelsUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limit, unlimited, err := parseSocialChannelLimit(test.value)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("parseSocialChannelLimit() error = %v, want %v", err, test.wantErr)
			}
			if limit != test.wantLimit || unlimited != test.wantUnlimited {
				t.Fatalf("parseSocialChannelLimit() = (%d, %t), want (%d, %t)", limit, unlimited, test.wantLimit, test.wantUnlimited)
			}
		})
	}
}
