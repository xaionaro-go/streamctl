package events

type Event = string

const (
	ConfigChange             Event = "config_change"
	StreamsChange            Event = "streams_change"
	StreamServersChange      Event = "servers_change"
	StreamDestinationsChange Event = "destinations_change"
	IncomingStreamsChange    Event = "incoming_streams_change"
	StreamForwardsChange     Event = "stream_forwards_change"
	StreamPlayersChange      Event = "stream_players_change"
	OBSCurrentProgramScene   Event = "obs_current_program_scene"
)
