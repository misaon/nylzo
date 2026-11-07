package core

import "regexp"

var emailAddress = regexp.MustCompile(`^[A-Za-z0-9_%+-]+(\.[A-Za-z0-9_%+-]+)*@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)

func IsEmailAddress(text string) bool {
	return emailAddress.MatchString(text)
}
