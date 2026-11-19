-- Token bucket. KEYS[1]=bucket anahtarı, ARGV[1]=saniyede dolan token (rate), ARGV[2]=kova kapasitesi (burst)
-- Zamanı Redis'ten alırız (TIME): birden fazla worker'ın saat farkı sonucu bozmaz.
local rate  = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])

local t   = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000) -- milisaniye

local data   = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts     = tonumber(data[2])
if tokens == nil then
  tokens = burst
  ts = now
end

-- Geçen süre kadar token ekle, kapasiteyi aşma
local elapsed = math.max(0, now - ts)
tokens = math.min(burst, tokens + elapsed * rate / 1000)

local allowed = 0
local wait = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  wait = math.ceil((1 - tokens) * 1000 / rate) -- bir token oluşana kadar ms
end

redis.call('HSET', KEYS[1], 'tokens', tokens, 'ts', now)
redis.call('PEXPIRE', KEYS[1], 10000) -- kullanılmayan kova 10 sn sonra silinsin
return {allowed, wait}
