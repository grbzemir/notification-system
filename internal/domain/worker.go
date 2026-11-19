package domain

import "errors"

// ErrNotClaimable bildirim sahiplenilemedi demektir: zaten gönderilmiş, iptal edilmiş,
// başka bir worker tarafından işleniyor ya da hiç yok. Hata değil, "yapacak iş yok" durumudur.
var ErrNotClaimable = errors.New("notification is not claimable")
