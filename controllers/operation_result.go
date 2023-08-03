package controllers

type CreatedUpdatedOrNoop string

const (
	Created CreatedUpdatedOrNoop = "created"
	Updated CreatedUpdatedOrNoop = "updated"
	Noop    CreatedUpdatedOrNoop = "noop"
)
