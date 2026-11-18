package main

// TRACK C · Feature C2 — Distributed lock for rider assignment.     Redis: SET NX PX + Lua
//
// Problem: we run 3 dispatcher instances. Two orders come in at the same time and both dispatchers
// pick Ravi, the nearest rider. Now Ravi has two orders on opposite sides of town.
// A sync.Mutex only protects one Go process. We need a lock that ALL instances respect.
//
// Keys you own:   lock:{name}            STRING  value = a random token, expires after ttlMs
//                 rider:{riderId}:order  STRING  the order a rider is currently delivering

func LockKey(name string) string {
	return "lock:" + name
}

// TODO(1): Try to take the lock. Don't wait.
//   token = a random uuid string (proves later that YOU own the lock)
//   SET lock:{name} token NX PX ttlMs     -> "OK" = you got it -> return token, nil
//                                          -> nil = someone else holds it -> return "", nil
//   Why PX (an expiry)? If this process crashes while holding the lock, it must free itself.
func TryLock(name string, ttlMs int64) (string, error) {
	panic("TODO(1) not implemented yet")
}

// TODO(2): Release the lock — but ONLY if it's still yours.
//   Danger: your lock expired (you were slow), someone else took it, and now you DEL *their* lock.
//   So: "if GET == my token then DEL" — and that check+delete must be atomic -> Lua.
//   Return true if the lock was yours and got deleted.
const UnlockLua = `
-- write your script here
`

func Unlock(name, token string) (bool, error) {
	panic("TODO(2) not implemented yet")
}

// TODO(3): Assign the first free rider from candidates (nearest first) to the order.
//   for each rider:
//     token, _ = TryLock("rider:" + rider, 5000);   "" -> someone is assigning this rider right now, skip
//     defer or ensure Unlock(...) is called!
//     free = (EXISTS rider:{rider}:order == 0)
//     if free -> SET rider:{rider}:order orderId and return rider, nil
//   nobody free -> return "", nil
//   (Is the lock even needed here? Think about SET ... NX on rider:{rider}:order. docs/TRACK-C.md Q4)
func AssignRider(orderID string, candidates []string) (string, error) {
	panic("TODO(3) not implemented yet")
}

// GIVEN: the rider delivered the order and is free again.
func ReleaseRider(riderID string) error {
	_, err := RedisCall("DEL", "rider:"+riderID+":order")
	return err
}
