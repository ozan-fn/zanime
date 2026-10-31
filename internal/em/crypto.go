// Kripto embedmaster, porting 1:1 dari em-crypto.min.js + skrip kredensial
// halaman player:
//
//	kredensial: base64 → XOR (base + key[i%len] + i) & 255 → UTF-8 → JSON
//	resolve:    base64url → nonce16 ‖ ciphertext;
//	            blok ke-c = SHA256(key ‖ nonce ‖ be32(c)); plaintext = ct XOR blok
//
// Keduanya kecil dan deterministik, jadi tidak perlu JS engine.
package em

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// credRe menangkap pola skrip kredensial terobfuscate:
// var <b64>="…", <key>=[…], <base>=<n>;
var credRe = regexp.MustCompile(`var\s+\w+\s*=\s*"([A-Za-z0-9+/=]+)"\s*,\s*\w+\s*=\s*\[([0-9,\s]+)\]\s*,\s*\w+\s*=\s*(\d+)`)

// credCfg adalah isi window.__EM_PLAYER__ yang kita pakai.
type credCfg struct {
	Ticket      string `json:"ticket"`
	PK          string `json:"pk"`
	ScrapeToken string `json:"scrapeToken"`
	FeedURL     string `json:"feedUrl"`
	ResolveURL  string `json:"resolveUrl"`

	raw string // payload mentah (untuk diagnosa)
}

// decodeCred membalik obfuscation skrip kredensial halaman player.
func decodeCred(script string) (credCfg, error) {
	var cfg credCfg
	m := credRe.FindStringSubmatch(script)
	if m == nil {
		return cfg, fmt.Errorf("pola kredensial tidak dikenal")
	}
	raw, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return cfg, fmt.Errorf("base64 kredensial: %w", err)
	}
	key, err := parseInts(m[2])
	if err != nil || len(key) == 0 {
		return cfg, fmt.Errorf("kunci XOR kredensial tidak valid")
	}
	base, err := strconv.Atoi(m[3])
	if err != nil {
		return cfg, fmt.Errorf("basis XOR kredensial tidak valid")
	}
	plain := make([]byte, len(raw))
	for i := range raw {
		plain[i] = raw[i] ^ byte((base+key[i%len(key)]+i)&0xff)
	}
	payload := string(plain)
	cfg.raw = payload
	start, end := strings.Index(payload, "{"), strings.LastIndex(payload, "}")
	if start < 0 || end <= start {
		return cfg, fmt.Errorf("payload kredensial bukan JSON")
	}
	if err := json.Unmarshal([]byte(payload[start:end+1]), &cfg); err != nil {
		return cfg, fmt.Errorf("payload kredensial: %w", err)
	}
	if cfg.Ticket == "" || cfg.PK == "" || cfg.FeedURL == "" || cfg.ResolveURL == "" {
		return cfg, fmt.Errorf("kredensial tidak lengkap")
	}
	return cfg, nil
}

func parseInts(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// decrypt membalik enkripsi resolve.php: base64url(blob) → nonce16 ‖ ct,
// lalu XOR dengan blok SHA256(key ‖ nonce ‖ counter big-endian).
func decrypt(blob, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) == 0 {
		return "", fmt.Errorf("pk bukan hex: %w", err)
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(blob), "="))
	if err != nil {
		return "", fmt.Errorf("blob bukan base64url: %w", err)
	}
	if len(data) < 16 {
		return "", fmt.Errorf("blob terlalu pendek (%d byte)", len(data))
	}
	nonce, ct := data[:16], data[16:]
	out := make([]byte, len(ct))
	var ctr [4]byte
	for i := 0; i*32 < len(ct); i++ {
		ctr[0] = byte(i >> 24)
		ctr[1] = byte(i >> 16)
		ctr[2] = byte(i >> 8)
		ctr[3] = byte(i)
		h := sha256.New()
		h.Write(key)
		h.Write(nonce)
		h.Write(ctr[:])
		block := h.Sum(nil)
		for j := 0; j < 32 && i*32+j < len(ct); j++ {
			out[i*32+j] = ct[i*32+j] ^ block[j]
		}
	}
	return string(out), nil
}
