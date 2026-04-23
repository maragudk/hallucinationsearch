package model

import "maragu.dev/glue/model"

const (
	ErrorEmailConflict = model.ErrorEmailConflict
	ErrorTokenExpired  = model.ErrorTokenExpired
	ErrorTokenNotFound = model.ErrorTokenNotFound
	ErrorUserInactive  = model.ErrorUserInactive
	ErrorUserNotFound  = model.ErrorUserNotFound
)

const (
	ErrorQueryNotFound   = Error("query not found")
	ErrorResultNotFound  = Error("result not found")
	ErrorWebsiteNotFound = Error("website not found")
)
