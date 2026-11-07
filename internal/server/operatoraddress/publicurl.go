package operatoraddress

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/misaon/nylzo/internal/loopback"
)

var (
	dnsName = regexp.MustCompile(
		`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`,
	)
	errPublicURLShape    = errors.New("want https://host or https://host:port with nothing after it")
	errPublicURLInsecure = errors.New("want https://; http:// is only for localhost or a loopback address")
	errNotPortNumber     = errors.New("is not a port number")
	errAddressZone       = errors.New("the address must not carry a zone")
	errHostName          = errors.New("must be a lowercase ASCII name or an IP address")
)

func CheckPublicURL(publicURL string) error {
	parsed, err := url.Parse(publicURL)
	if err != nil || parsed.Scheme+"://"+parsed.Host != publicURL || strings.HasSuffix(parsed.Host, ":") {
		return errPublicURLShape
	}
	host := parsed.Hostname()
	if portErr := checkPort(parsed.Port()); portErr != nil {
		return portErr
	}
	if hostErr := checkHost(host); hostErr != nil {
		return hostErr
	}
	if parsed.Scheme == "https" || parsed.Scheme == "http" && loopback.Host(host) {
		return nil
	}
	return errPublicURLInsecure
}

func checkPort(port string) error {
	if port == "" {
		return nil
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
		return fmt.Errorf("%q %w", port, errNotPortNumber)
	}
	return nil
}

func checkHost(host string) error {
	if address, err := netip.ParseAddr(host); err == nil {
		if address.Zone() != "" {
			return errAddressZone
		}
		return nil
	}
	if len(host) > 253 || !dnsName.MatchString(host) {
		return fmt.Errorf("the host %q %w", host, errHostName)
	}
	return nil
}
