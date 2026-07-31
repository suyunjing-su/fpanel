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
	mptcp     bool
	backlog   int
	session   coretot.Options
	handshake coretot.HandshakeOptions
	idleTTL   time.Duration
}

func (l *totListener) parseMetadata(md md.Metadata) error {
	secret := strings.TrimSpace(mdutil.GetString(md, "secret", "key"))
	if len(secret) < 16 {
		return errors.New("TOT secret must contain at least 16 bytes")
	}
	l.md.mptcp = mdutil.GetBool(md, "mptcp")
	l.md.backlog = mdutil.GetInt(md, "backlog")
	if l.md.backlog <= 0 {
		l.md.backlog = 128
	}
	l.md.session = coretot.Options{
		Key:                []byte(secret),
		MaxPayload:         mdutil.GetInt(md, "maxPayload", "frameSize"),
		Window:             mdutil.GetInt(md, "window", "sendWindow"),
		RetransmitInterval: mdutil.GetDuration(md, "retransmitInterval", "retransmit.interval"),
		MaxRetries:         mdutil.GetInt(md, "maxRetries", "retransmit.maxRetries"),
	}
	l.md.handshake = coretot.HandshakeOptions{
		Secret:       []byte(secret),
		Timeout:      mdutil.GetDuration(md, "handshakeTimeout"),
		MaxClockSkew: mdutil.GetDuration(md, "maxClockSkew"),
	}
	l.md.idleTTL = mdutil.GetDuration(md, "idleTTL", "sessionIdleTTL")
	return nil
}
