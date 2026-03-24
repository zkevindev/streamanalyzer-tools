//
// Copyright (c) 2018- yutopp (yutopp@gmail.com)
//
// Distributed under the Boost Software License, Version 1.0. (See accompanying
// file LICENSE_1_0.txt or copy at  https://www.boost.org/LICENSE_1_0.txt)
//

package rtmp

import (
	"github.com/yutopp/go-rtmp/internal"
	"github.com/yutopp/go-rtmp/message"
)

var _ stateHandler = (*clientControlConnectedHandler)(nil)

// clientControlConnectedHandler Handle messages from a server after client connected.
// Used for receiving play data.
//
//	transitions:
//	  | _ -> self
type clientControlConnectedHandler struct {
	sh *streamHandler
}

func (h *clientControlConnectedHandler) onMessage(
	chunkStreamID int,
	timestamp uint32,
	msg message.Message,
) error {
	switch msg := msg.(type) {
	case *message.AudioMessage:
		return h.sh.stream.userHandler().OnAudio(timestamp, msg.Payload)

	case *message.VideoMessage:
		return h.sh.stream.userHandler().OnVideo(timestamp, msg.Payload)

	default:
		return internal.ErrPassThroughMsg
	}
}

func (h *clientControlConnectedHandler) onData(
	chunkStreamID int,
	timestamp uint32,
	dataMsg *message.DataMessage,
	body interface{},
) error {
	switch data := body.(type) {
	case *message.NetStreamSetDataFrame:
		return h.sh.stream.userHandler().OnSetDataFrame(timestamp, data)

	default:
		return internal.ErrPassThroughMsg
	}
}

func (h *clientControlConnectedHandler) onCommand(
	chunkStreamID int,
	timestamp uint32,
	cmdMsg *message.CommandMessage,
	body interface{},
) error {
	switch cmd := body.(type) {
	case *message.NetConnectionCreateStreamResult:
		h.sh.Logger().Infof("CreateStreamResult: StreamID = %d", cmd.StreamID)
		return nil

	case *message.NetStreamOnStatus:
		h.sh.Logger().Infof("OnStatus: Level = %s, Code = %s, Description = %s",
			cmd.InfoObject.Level, cmd.InfoObject.Code, cmd.InfoObject.Description)
		return nil

	default:
		return internal.ErrPassThroughMsg
	}
}
