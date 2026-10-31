package widgetapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"
)

var ErrInvalidPairCode = errors.New("pairing code is invalid, expired, or already used")

type pairingRecord struct {
	actorID int64
	expires time.Time
}

// Pairing issues short-lived one-time codes. The permanent API token is only
// returned by the HTTPS exchange endpoint and is never put into a Telegram URL.
type Pairing struct {
	mu        sync.Mutex
	codes     map[[sha256.Size]byte]pairingRecord
	token     string
	publicURL string
	ttl       time.Duration
	now       func() time.Time
}

func NewPairing(token, publicURL string, ttl time.Duration) *Pairing {
	return &Pairing{
		codes:     make(map[[sha256.Size]byte]pairingRecord),
		token:     token,
		publicURL: strings.TrimRight(publicURL, "/"),
		ttl:       ttl,
		now:       time.Now,
	}
}

func (p *Pairing) CreatePairingURL(actorID int64) (string, time.Time, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", time.Time{}, err
	}
	code := base64.RawURLEncoding.EncodeToString(random)
	hash := sha256.Sum256([]byte(code))
	expires := p.now().Add(p.ttl)

	p.mu.Lock()
	p.removeExpiredLocked()
	p.codes[hash] = pairingRecord{actorID: actorID, expires: expires}
	p.mu.Unlock()

	return p.publicURL + "/widget/setup?code=" + url.QueryEscape(code), expires, nil
}

func (p *Pairing) Redeem(code string) (string, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil || len(decoded) != 32 {
		return "", "", ErrInvalidPairCode
	}
	hash := sha256.Sum256([]byte(code))

	p.mu.Lock()
	record, ok := p.codes[hash]
	delete(p.codes, hash)
	p.removeExpiredLocked()
	p.mu.Unlock()
	if !ok || !p.now().Before(record.expires) {
		return "", "", ErrInvalidPairCode
	}
	return p.publicURL, p.token, nil
}

func (p *Pairing) removeExpiredLocked() {
	now := p.now()
	for hash, record := range p.codes {
		if !now.Before(record.expires) {
			delete(p.codes, hash)
		}
	}
}
