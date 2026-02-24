package app

import "errors"

var (
	errMultiAgentIDsInvalid    = errors.New("multi_agent_ids_invalid")
	errMultiAgentItemsInvalid  = errors.New("multi_agent_items_invalid")
	errMultiAgentInputConflict = errors.New("multi_agent_input_conflict")
	errMultiAgentClosed        = errors.New("multi_agent_closed")
)
