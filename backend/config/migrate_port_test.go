package config

import "testing"

func TestLegacyDatabasePortBoundaries(t *testing.T) {
	for _, test := range []struct {
		value string
		want  uint16
		valid bool
	}{
		{value: "", want: 3306, valid: true},
		{value: "1", want: 1, valid: true},
		{value: "65535", want: 65535, valid: true},
		{value: "0"},
		{value: "-1"},
		{value: "65536"},
		{value: "18446744073709551616"},
		{value: " 3306"},
	} {
		port, valid := parseLegacyDatabasePort(test.value, DefaultMySQLPort)
		if port != test.want || valid != test.valid {
			t.Fatalf("端口 %q 的解析结果错误：%d %t", test.value, port, valid)
		}
	}
	for _, value := range []string{"0", "65536", "18446744073709551616"} {
		if _, _, valid := parseLegacyMySQLDSN("smail:sample@tcp(db.example.test:" + value + ")/smail"); valid {
			t.Fatal("MySQL 连接串接受了越界端口")
		}
		if _, _, valid := parseLegacyPostgreSQLDSN("postgres://smail:sample@db.example.test:" + value + "/smail"); valid {
			t.Fatal("PostgreSQL 连接串接受了越界端口")
		}
	}
	if _, valid := parseLegacyDatabasePort("", 0); valid {
		t.Fatal("默认端口不能为零")
	}
}
