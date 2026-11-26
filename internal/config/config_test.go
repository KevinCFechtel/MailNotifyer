package config

import (
	"strings"
	"testing"

	"github.com/KevinCFechtel/MailNotifyer/internal/mailparser"
)

func TestLoadUnattendedUpgradesTrimFrom(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    mailparser.UnattendedUpgradesTrimFrom
		wantErr bool
	}{
		{name: "default", want: mailparser.TrimPackageInstallationLog},
		{name: "package log", value: "package-installation-log", want: mailparser.TrimPackageInstallationLog},
		{name: "unattended log", value: "unattended-upgrades-log", want: mailparser.TrimUnattendedUpgradesLog},
		{name: "disabled", value: "none", want: mailparser.TrimNone},
		{name: "normalized", value: " NONE ", want: mailparser.TrimNone},
		{name: "invalid", value: "invalid", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PUSHOVER_TOKEN", "test-token")
			t.Setenv("PUSHOVER_USER", "test-user")
			t.Setenv("SMTP_MAX_MESSAGE_BYTES", "")
			t.Setenv("PUSHOVER_TIMEOUT", "")
			t.Setenv("UNATTENDED_UPGRADES_TRIM_FROM", test.value)

			got, err := Load()
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "UNATTENDED_UPGRADES_TRIM_FROM") {
					t.Fatalf("Load() error = %v, want invalid trim configuration", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.UnattendedUpgradesTrimFrom != test.want {
				t.Errorf("UnattendedUpgradesTrimFrom = %q, want %q", got.UnattendedUpgradesTrimFrom, test.want)
			}
		})
	}
}

func TestLoadPushoverEncryptionKey(t *testing.T) {
	t.Setenv("PUSHOVER_TOKEN", "test-token")
	t.Setenv("PUSHOVER_USER", "test-user")
	t.Setenv("SMTP_MAX_MESSAGE_BYTES", "")
	t.Setenv("PUSHOVER_TIMEOUT", "")
	t.Setenv("UNATTENDED_UPGRADES_TRIM_FROM", "")
	for _, value := range []string{"", "  " + strings.Repeat("ab", 32) + "  "} {
		t.Setenv("PUSHOVER_ENCRYPTION_KEY", value)
		got, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.PushoverEncryptionKey != strings.TrimSpace(value) {
			t.Error("PushoverEncryptionKey does not match trimmed environment value")
		}
	}
}
