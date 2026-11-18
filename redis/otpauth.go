package main

import (
	"errors"
)

// TRACK B · Feature B2 — OTP login + sessions.              Redis types: STRING with TTL, counter, HASH
//
// Problem: users log in with a phone number + 6-digit SMS code. Codes must expire, users must not
// spam "resend", attackers must not brute-force 1,000,000 codes, and after login every API server
// must recognise the session token. All of these are "small data with an expiry" — Redis's sweet spot.
//
// Keys you own:   otp:{phone}            STRING  the code            expires in 120 s
//                 otp:cooldown:{phone}   STRING  "resend" blocker    expires in 30 s
//                 otp:attempts:{phone}   STRING  wrong-guess counter expires in 120 s
//                 session:{token}        HASH    phone, createdAt    expires 30 min after last use
//
//   POST /auth/otp     {"phone":"9876543210"}               -> 200 (code is printed in the server log = "SMS")
//   POST /auth/verify  {"phone":"...","code":"123456"}      -> 200 {"token": "..."}
//   GET  /auth/me      header Authorization: Bearer <token>
//   POST /auth/logout  header Authorization: Bearer <token>

const (
	OtpTTLSeconds         = 120
	ResendCooldownSeconds = 30
	MaxAttempts           = 3
	SessionTTLSeconds     = 30 * 60
)

// ErrCooldown indicates a user is requesting an OTP too frequently.
var ErrCooldown = errors.New("wait before requesting another code")

// VerifyResult represents the status of an OTP verification attempt.
// status: "OK", "WRONG_CODE", "EXPIRED", "LOCKED".
type VerifyResult struct {
	Status string `json:"status"`
	Token  string `json:"token,omitempty"`
}

// TODO(1): Send an OTP.
//   1. Cooldown: SET otp:cooldown:{phone} 1 NX EX RESEND_COOLDOWN_SECONDS
//      NX means "only if it doesn't exist". Reply is "OK" if set, nil if it already existed.
//      nil -> return "", ErrCooldown
//   2. code = 6 random digits: fmt.Sprintf("%06d", rand.Intn(1_000_000)) (or crypto/rand)
//   3. SET otp:{phone} code EX OTP_TTL_SECONDS     and  DEL otp:attempts:{phone}  (fresh code, fresh attempts)
//   4. fmt.Printf("[SMS to %s] Your QuickBite code is %s\n", phone, code); return code, nil
func RequestOtp(phone string) (string, error) {
	panic("TODO(1) not implemented yet")
}

// TODO(2): Verify a code.
//   1. attempts = INCR otp:attempts:{phone};  if attempts == 1 -> EXPIRE it OTP_TTL_SECONDS
//      attempts > MAX_ATTEMPTS -> return VerifyResult{Status: "LOCKED"}, nil   (count BEFORE checking, so even a correct 4th guess is refused)
//   2. stored = GET otp:{phone};  nil/empty -> return VerifyResult{Status: "EXPIRED"}, nil;  different from code -> return VerifyResult{Status: "WRONG_CODE"}, nil
//   3. Correct: DEL otp:{phone} and otp:attempts:{phone}  (a code works exactly once)
//      token = uuid string (or crypto random token)
//      HSET session:{token} phone {phone} createdAt {unix_millis}    then EXPIRE it SESSION_TTL_SECONDS
//      return VerifyResult{Status: "OK", Token: token}, nil
func VerifyOtp(phone, code string) (VerifyResult, error) {
	panic("TODO(2) not implemented yet")
}

// TODO(3): Look up a session (every authenticated request calls this).
//   HGETALL session:{token} -> empty list / error means no session: return nil, nil.
//   Otherwise push the expiry back to SESSION_TTL_SECONDS (sliding: active users stay logged in)
//   and return the fields as a map[string]string (phone -> ..., createdAt -> ...).
func Session(token string) (map[string]string, error) {
	if token == "" {
		return nil, nil
	}
	panic("TODO(3) not implemented yet")
}

// TODO(4): Logout = delete the session. Return true if a session was actually deleted (DEL session:{token} > 0).
func Logout(token string) (bool, error) {
	panic("TODO(4) not implemented yet")
}
