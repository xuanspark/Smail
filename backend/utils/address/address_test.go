package address

import "testing"

func TestIsValidEmailAddress(t *testing.T) {
	type args struct {
		str string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"",
			args{"test@qq.com"},
			true,
		},
		{
			"",
			args{"1000@qq.com"},
			true,
		},
		{
			"",
			args{"1000@163.com"},
			true,
		},
		{
			"",
			args{"1000@1631com"},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidEmailAddress(tt.args.str); got != tt.want {
				t.Errorf("IsValidEmailAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidEmailAddressRejectsMalformedAddresses(t *testing.T) {
	invalid := []string{
		"@example.com",
		"user@",
		"user..name@example.com",
		"user@example..com",
		"user name@example.com",
		"User <user@example.com>",
		" user@example.com",
		"user@example.com ",
		".user@example.com",
		"user.@example.com",
		"user@example.com.",
	}
	for _, value := range invalid {
		if IsValidEmailAddress(value) {
			t.Errorf("IsValidEmailAddress(%q) = true, want false", value)
		}
	}
}
