package appliance

import "errors"

var errOwnerLockContended = errors.New("owner lock is already held")
