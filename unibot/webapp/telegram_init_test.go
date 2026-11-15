package webapp

import "testing"

func TestValidateTelegramInitDataOfficialSample(t *testing.T) {
	const botToken = "5768337691:AAH5YkoiEuPk8-FZa32hStHTqXiLPtAEhx8"
	const raw = "query_id=AAHdF6IQAAAAAN0XohDhrOrc&user=%7B%22id%22%3A279058397%2C%22first_name%22%3A%22Vladislav%22%2C%22last_name%22%3A%22Kibenko%22%2C%22username%22%3A%22vdkfrost%22%2C%22language_code%22%3A%22ru%22%2C%22is_premium%22%3Atrue%7D&auth_date=1662771648&hash=c501b71e775f74ce10e377dea85a7ea24ecd640b223ea86dfe453e0eaed2e2b2"

	values, err := ValidateTelegramInitData(raw, botToken, 0)
	if err != nil {
		t.Fatalf("ValidateTelegramInitData: %v", err)
	}
	if got := values.Get("query_id"); got != "AAHdF6IQAAAAAN0XohDhrOrc" {
		t.Fatalf("query_id = %q", got)
	}
}

func TestValidateTelegramInitDataRejectsTamper(t *testing.T) {
	const botToken = "5768337691:AAH5YkoiEuPk8-FZa32hStHTqXiLPtAEhx8"
	const raw = "query_id=changed&user=%7B%22id%22%3A279058397%7D&auth_date=1662771648&hash=c501b71e775f74ce10e377dea85a7ea24ecd640b223ea86dfe453e0eaed2e2b2"
	if _, err := ValidateTelegramInitData(raw, botToken, 0); err == nil {
		t.Fatal("expected tampered initData to be rejected")
	}
}
