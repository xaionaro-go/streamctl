package ffstream

/*

		outputStreams := e.OutputStreams[output]
		if outputStreams == nil {
			outputStreams = map[int]*astiav.Stream{}
			e.OutputStreams[output] = outputStreams
		}

		outputStream, ok := outputStreams[input.Packet.StreamIndex()]
		if !ok {
			outputStream = output.FormatContext.NewStream(nil)
			if outputStream == nil {
				return fmt.Errorf("internal error: the newly created output stream is nil")
			}

			if err := inputStream.CodecParameters().Copy(outputStream.CodecParameters()); err != nil {
				return fmt.Errorf("unable to copy the codec parameters of stream #%d: %w", input.Packet.StreamIndex(), err)
			}

			outputStream.CodecParameters().SetCodecTag(0)
			outputStreams[inputStream.Index()] = outputStream
			if err := output.FormatContext.WriteHeader(nil); err != nil {
				return fmt.Errorf("unable to write the header to the output: %w", err)
			}
		}

		//input.Packet.SetStreamIndex(outputStream.Index())
		inputPacket.Packet.RescaleTs(inputStream.TimeBase(), outputStream.TimeBase())
		inputPacket.SetPos(-1)


func (r *EncoderLoop) rerouteTracks(

	ctx context.Context,

	) error {
		for _, output := range r.outputs {
			for aIdx, track := range r.cfg.OutputAudioTracks {
				if output.routedAudioTracks[aIdx] {
					continue
				}
				inputStreams := r.inputs[track.InputID]
				var inputStream *astiav.Stream
				for _, trackID := range track.InputTrackIDs {
					_inputStream, ok := inputStreams.streams[trackID]
					if !ok {
						continue
					}
					mediaType := _inputStream.CodecParameters().MediaType()
					if mediaType != astiav.MediaTypeAudio {
						continue
					}
					inputStream = _inputStream
					break
				}
				if inputStream == nil {
					return fmt.Errorf("unable to find an appropriate input for the %d audio track", aIdx)
				}

				outputStream := output.output.FormatContext.NewStream(nil)
				if outputStream == nil {
					return fmt.Errorf("the output stream is nil")
				}

				streams[inputStream.Index()] = outputStream

				track.InputID
			}

			for vIdx, track := range r.cfg.OutputVideoTracks {
				if inputStream.CodecParameters().MediaType() != astiav.MediaTypeAudio &&
					inputStream.CodecParameters().MediaType() != astiav.MediaTypeVideo {
					continue
				}
				inputStreams[inputStream.Index()] = inputStream

				outputStream := output.FormatContext.NewStream(nil)
				if outputStream == nil {
					return fmt.Errorf("the output stream is nil")
				}

				if err := inputStream.CodecParameters().Copy(outputStream.CodecParameters()); err != nil {
					return fmt.Errorf("unable to copy the codec parameters: %w", err)
				}

				outputStream.CodecParameters().SetCodecTag(0)
				streams[inputStream.Index()] = outputStream
			}

			/ *		if !modifiedSomething {
						continue
					}

					if err := output.output.FormatContext.WriteHeader(nil); err != nil {
						return fmt.Errorf("unable to write the header to the output: %w", err)
					}* /
		}

		return nil
	}
*/

/*
	e.Locker.Lock()
	defer e.Locker.Unlock()

	inputStream, ok := e.InputStreams[input.Packet.StreamIndex()]
	if !ok {
		for _, inputStream := range input.Input.FormatContext.Streams() {
			if _, ok := e.InputStreams[inputStream.Index()]; ok {
				continue
			}
			e.InputStreams[inputStream.Index()] = inputStream
		}
	}

	inputStream, ok = e.InputStreams[input.Packet.StreamIndex()]
	if !ok {
		return nil, fmt.Errorf("received a packet of an unknown stream ID: %d", input.Packet.StreamIndex())
	}

	mediaType := inputStream.CodecParameters().MediaType()
	switch mediaType {
	case astiav.MediaTypeAudio:
	case astiav.MediaTypeVideo:
	default:
		logger.Debugf(ctx, "skipping stream #%d, because it is neither audio, nor video: %v", input.Packet.StreamIndex(), mediaType)
		return nil, nil
	}
*/
