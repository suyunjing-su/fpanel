package tot

import (
	"errors"
	"strings"
	"time"

	md "github.com/go-gost/core/metadata"
	coretot "github.com/go-gost/x/internal/util/tot"
	mdutil "github.com/go-gost/x/metadata/util"
)

type metadata struct {
	secret         string
	paths          []string
	pathCount      int
	recoveryPeriod time.Duration
	session        coretot.Options
	handshake      coretot.HandshakeOptions
}

func (d *totDialer) parseMetadata(md md.Metadata) error {
	d.md.secret = strings.TrimSpace(mdutil.GetString(md, "secret", "key"))
	if len(d.md.secret) < 16 {
		return errors.New("TOT secret must contain at least 16 bytes")
	}
	d.md.paths = normalizePaths(mdutil.GetStrings(md, "paths", "addresses"))
	d.md.pathCount = mdutil.GetInt(md, "pathCount", "paths.count")
	if d.md.pathCount <= 0 {
		d.md.pathCount = 2
	}
	d.md.recoveryPeriod = mdutil.GetDuration(md, "recoveryPeriod", "recovery.period")
	if d.md.recoveryPeriod <= 0 {
		d.md.recoveryPeriod = time.Second
	}
	d.md.session = coretot.Options{
		MaxPayload:         mdutil.GetInt(md, "maxPayload", "frameSize"),
		Window:             mdutil.GetInt(md, "window", "sendWindow"),
		RetransmitInterval: mdutil.GetDuration(md, "retransmitInterval", "retransmit.interval"),
		MaxRetries:         mdutil.GetInt(md, "maxRetries", "retransmit.maxRetries"),
	}
	d.md.handshake = coretot.HandshakeOptions{
		Secret:       []byte(d.md.secret),
		Timeout:      mdutil.GetDuration(md, "handshakeTimeout"),
		MaxClockSkew: mdutil.GetDuration(md, "maxClockSkew"),
	}
	return nil
}

func normalizePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		for _, value := range strings.Split(path, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}
