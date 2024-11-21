redis.replicate_commands()

local key = KEYS[1]
local rate = tonumber(ARGV[1])
local cost = tonumber(ARGV[2])
local burst_offset = tonumber(ARGV[3])

-- Get current time as seconds and microseconds from Redis
-- Convert to float for precision; adjust the epoch to be relative
-- to Jan 1, 2020 00:00:00 GMT to avoid floating point problems.
local jan_1_2020 = 1577836800
local rtime = redis.call("TIME")
local now = (rtime[1] - jan_1_2020) + (rtime[2] / 1000000)

local tat = tonumber(redis.call("GET", key)) or now
local new_tat = math.max(tat, now) + rate*cost
local allow_at = new_tat - burst_offset
local diff = now - allow_at
local remaining = diff / rate
local reset_after = math.ceil(new_tat - now)

if remaining < 0 then
    local retry_after = diff * -1
    return {
        tostring(false),
        0, -- no token remaining
        tostring(retry_after),
        tostring(reset_after),
    }
end

-- Request allowed, update Redis state with new TAT and expiration
if reset_after > 0 then
  redis.call("SETEX", key, reset_after, tostring(new_tat))
end

return {
    tostring(true),
    remaining,
    "0", -- no retry required
    tostring(reset_after),
}
