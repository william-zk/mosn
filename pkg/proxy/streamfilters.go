package proxy

import (
	"gitlab.alipay-inc.com/afe/mosn/pkg/types"
	"gitlab.alipay-inc.com/afe/mosn/pkg/network/buffer"
)

func (s *activeStream) addEncodedData(filter *activeStreamEncoderFilter, data types.IoBuffer, streaming bool) {
	if s.filterStage == 0 || s.filterStage&EncodeHeaders > 0 ||
		s.filterStage&EncodeData > 0 {
		s.encoderFiltersStreaming = streaming

		filter.handleBufferData(data)
	} else if s.filterStage&EncodeTrailers > 0 {
		s.encodeDataFilters(filter, data, false)
	}
}

func (s *activeStream) addDecodedData(filter *activeStreamDecoderFilter, data types.IoBuffer, streaming bool) {
	if s.filterStage == 0 || s.filterStage&DecodeHeaders > 0 ||
		s.filterStage&DecodeData > 0 {
		s.decoderFiltersStreaming = streaming

		filter.handleBufferData(data)
	} else if s.filterStage&EncodeTrailers > 0 {
		s.decodeDataFilters(filter, data, false)
	}
}

func (s *activeStream) encodeHeaderFilters(filter *activeStreamEncoderFilter, headers map[string]string, endStream bool) bool {
	var index int
	var f *activeStreamEncoderFilter

	if filter != nil {
		index = filter.index + 1
	}

	for ; index < len(s.encoderFilters); index++ {
		f = s.encoderFilters[index]

		s.filterStage |= EncodeHeaders
		status := f.filter.EncodeHeaders(headers, endStream)
		s.filterStage &= ^EncodeHeaders

		if status == types.FilterHeadersStatusStopIteration {
			f.filterStopped = true

			return true
		} else {
			f.headersContinued = true

			return false
		}
	}

	return false
}

func (s *activeStream) encodeDataFilters(filter *activeStreamEncoderFilter, data types.IoBuffer, endStream bool) bool {
	var index int
	var f *activeStreamEncoderFilter

	if filter != nil {
		index = filter.index + 1
	}

	for ; index < len(s.encoderFilters); index++ {
		f = s.encoderFilters[index]

		s.filterStage |= EncodeData
		status := f.filter.EncodeData(data, endStream)
		s.filterStage &= ^EncodeData

		if status == types.FilterDataStatusContinue {
			if f.filterStopped {
				f.handleBufferData(data)
				f.doContinue()

				return true
			}
		} else {
			f.filterStopped = true

			if status == types.FilterDataStatusStopIterationAndBuffer ||
				status == types.FilterDataStatusStopIterationAndWatermark {
				s.encoderFiltersStreaming = status == types.FilterDataStatusStopIterationAndWatermark
				f.handleBufferData(data)
			}

			return true
		}
	}

	return false
}

func (s *activeStream) encodeTrailersFilters(filter *activeStreamEncoderFilter, trailers map[string]string) bool {
	var index int
	var f *activeStreamEncoderFilter

	if filter != nil {
		index = filter.index + 1
	}

	for ; index < len(s.encoderFilters); index++ {
		f = s.encoderFilters[index]

		s.filterStage |= EncodeTrailers
		status := f.filter.EncodeTrailers(trailers)
		s.filterStage &= ^EncodeTrailers

		if status == types.FilterTrailersStatusContinue {
			if f.filterStopped {
				f.doContinue()

				return true
			}
		} else {
			return true
		}
	}

	return false
}

func (s *activeStream) decodeHeaderFilters(filter *activeStreamDecoderFilter, headers map[string]string, endStream bool) bool {
	var index int
	var f *activeStreamDecoderFilter

	if filter != nil {
		index = filter.index + 1
	}

	for ; index < len(s.decoderFilters); index++ {
		f = s.decoderFilters[index]

		s.filterStage |= DecodeHeaders
		status := f.filter.DecodeHeaders(headers, endStream)
		s.filterStage &= ^DecodeHeaders

		if status == types.FilterHeadersStatusStopIteration {
			f.filterStopped = true

			return true
		} else {
			f.headersContinued = true

			return false
		}
	}

	return false
}

func (s *activeStream) decodeDataFilters(filter *activeStreamDecoderFilter, data types.IoBuffer, endStream bool) bool {
	if s.localProcessDone {
		return false
	}

	var index int
	var f *activeStreamDecoderFilter

	if filter != nil {
		index = filter.index + 1
	}

	for ; index < len(s.decoderFilters); index++ {
		f = s.decoderFilters[index]

		s.filterStage |= DecodeData
		status := f.filter.DecodeData(data, endStream)
		s.filterStage &= ^DecodeData

		if status == types.FilterDataStatusContinue {
			if f.filterStopped {
				f.handleBufferData(data)
				f.doContinue()

				return false
			}
		} else {
			f.filterStopped = true

			if status == types.FilterDataStatusStopIterationAndBuffer ||
				status == types.FilterDataStatusStopIterationAndWatermark {
				s.decoderFiltersStreaming = status == types.FilterDataStatusStopIterationAndWatermark
				f.handleBufferData(data)
			}

			return true
		}
	}

	return false
}

func (s *activeStream) decodeTrailersFilters(filter *activeStreamDecoderFilter, trailers map[string]string) bool {
	if s.localProcessDone {
		return false
	}

	var index int
	var f *activeStreamDecoderFilter

	if filter != nil {
		index = filter.index + 1
	}

	for ; index < len(s.decoderFilters); index++ {
		f = s.decoderFilters[index]

		s.filterStage |= DecodeTrailers
		status := f.filter.DecodeTrailers(trailers)
		s.filterStage &= ^DecodeTrailers

		if status == types.FilterTrailersStatusContinue {
			if f.filterStopped {
				f.doContinue()

				return false
			}
		} else {
			return true
		}
	}

	return false
}

type FilterStage int

const (
	DecodeHeaders  = iota
	DecodeData
	DecodeTrailers
	EncodeHeaders
	EncodeData
	EncodeTrailers
)

// types.StreamFilterCallbacks
type activeStreamFilter struct {
	index int

	activeStream     *activeStream
	filterStopped    bool
	headersContinued bool
}

func (f *activeStreamFilter) Connection() types.Connection {
	return f.activeStream.proxy.readCallbacks.Connection()
}

func (f *activeStreamFilter) ResetStream() {
	f.activeStream.resetStream()
}

func (f *activeStreamFilter) Route() types.Route {
	return f.activeStream.route
}

func (f *activeStreamFilter) StreamId() uint32 {
	return f.activeStream.streamId
}

func (f *activeStreamFilter) RequestInfo() types.RequestInfo {
	return f.activeStream.requestInfo
}

// types.StreamDecoderFilterCallbacks
type activeStreamDecoderFilter struct {
	activeStreamFilter

	filter types.StreamDecoderFilter
}

func newActiveStreamDecoderFilter(idx int, activeStream *activeStream,
	filter types.StreamDecoderFilter) *activeStreamDecoderFilter {
	f := &activeStreamDecoderFilter{
		activeStreamFilter: activeStreamFilter{
			index:        idx,
			activeStream: activeStream,
		},
		filter: filter,
	}
	filter.SetDecoderFilterCallbacks(f)

	return f
}

func (f *activeStreamDecoderFilter) ContinueDecoding() {
	f.doContinue()
}

func (f *activeStreamDecoderFilter) doContinue() {
	if f.activeStream.localProcessDone {
		return
	}

	f.filterStopped = false
	hasBuffedData := f.activeStream.downstreamReqDataBuf != nil
	hasTrailer := f.activeStream.downstreamReqTrailers != nil

	if !f.headersContinued {
		f.headersContinued = true

		endStream := f.activeStream.downstreamRecvDone && !hasBuffedData && !hasTrailer
		f.activeStream.doDecodeHeaders(f, f.activeStream.downstreamReqHeaders, endStream)
	}

	if hasBuffedData {
		endStream := f.activeStream.downstreamRecvDone && !hasTrailer
		f.activeStream.doDecodeData(f, f.activeStream.downstreamReqDataBuf, endStream)
	}

	if hasTrailer {
		f.activeStream.doDecodeTrailers(f, f.activeStream.downstreamReqTrailers)
	}
}

func (f *activeStreamDecoderFilter) handleBufferData(buf types.IoBuffer) {
	if f.activeStream.downstreamReqDataBuf != buf {
		if f.activeStream.downstreamReqDataBuf == nil {
			f.activeStream.downstreamReqDataBuf = buffer.NewIoBuffer(buf.Len())
		}

		f.activeStream.downstreamReqDataBuf.ReadFrom(buf)
	}
}

func (f *activeStreamDecoderFilter) DecodingBuffer() types.IoBuffer {
	return f.activeStream.downstreamReqDataBuf
}

func (f *activeStreamDecoderFilter) AddDecodedData(buf types.IoBuffer, streamingFilter bool) {
	f.activeStream.addDecodedData(f, buf, streamingFilter)
}

func (f *activeStreamDecoderFilter) EncodeHeaders(headers map[string]string, endStream bool) {
	f.activeStream.downstreamRespHeaders = headers
	f.activeStream.encodeHeaderFilters(nil, headers, endStream)
}

func (f *activeStreamDecoderFilter) EncodeData(buf types.IoBuffer, endStream bool) {
	f.activeStream.encodeDataFilters(nil, buf, endStream)
}

func (f *activeStreamDecoderFilter) EncodeTrailers(trailers map[string]string) {
	f.activeStream.downstreamRespTrailers = trailers
	f.activeStream.encodeTrailersFilters(nil, trailers)
}

func (f *activeStreamDecoderFilter) OnDecoderFilterAboveWriteBufferHighWatermark() {
	// todo
}

func (f *activeStreamDecoderFilter) OnDecoderFilterBelowWriteBufferLowWatermark() {
	// todo
}

func (f *activeStreamDecoderFilter) AddDownstreamWatermarkCallbacks(cb types.DownstreamWatermarkCallbacks) {
	// todo
}

func (f *activeStreamDecoderFilter) RemoveDownstreamWatermarkCallbacks(cb types.DownstreamWatermarkCallbacks) {
	// todo
}

func (f *activeStreamDecoderFilter) SetDecoderBufferLimit(limit uint32) {
	// todo
}

func (f *activeStreamDecoderFilter) DecoderBufferLimit() uint32 {
	// todo
	return 0
}

// types.StreamEncoderFilterCallbacks
type activeStreamEncoderFilter struct {
	activeStreamFilter

	filter types.StreamEncoderFilter
}

func newActiveStreamEncoderFilter(idx int, activeStream *activeStream,
	filter types.StreamEncoderFilter) *activeStreamEncoderFilter {
	f := &activeStreamEncoderFilter{
		activeStreamFilter: activeStreamFilter{
			index:        idx,
			activeStream: activeStream,
		},
		filter: filter,
	}

	filter.SetEncoderFilterCallbacks(f)

	return f
}

func (f *activeStreamEncoderFilter) ContinueEncoding() {
	f.doContinue()
}

func (f *activeStreamEncoderFilter) doContinue() {
	f.filterStopped = false
	hasBuffedData := f.activeStream.downstreamRespDataBuf != nil
	hasTrailer := f.activeStream.downstreamRespTrailers == nil

	if !f.headersContinued {
		f.headersContinued = true
		endStream := f.activeStream.localProcessDone && !hasBuffedData && !hasTrailer
		f.activeStream.doEncodeHeaders(f, f.activeStream.downstreamRespHeaders, endStream)
	}

	if hasBuffedData {
		endStream := f.activeStream.downstreamRecvDone && !hasTrailer
		f.activeStream.doEncodeData(f, f.activeStream.downstreamRespDataBuf, endStream)
	}

	if hasTrailer {
		f.activeStream.doEncodeTrailers(f, f.activeStream.downstreamRespTrailers)
	}
}

func (f *activeStreamEncoderFilter) handleBufferData(buf types.IoBuffer) {
	if f.activeStream.downstreamRespDataBuf != buf {
		if f.activeStream.downstreamRespDataBuf == nil {
			f.activeStream.downstreamRespDataBuf = buffer.NewIoBuffer(buf.Len())
		}

		f.activeStream.downstreamRespDataBuf.ReadFrom(buf)
	}
}

func (f *activeStreamEncoderFilter) EncodingBuffer() types.IoBuffer {
	return f.activeStream.downstreamRespDataBuf
}

func (f *activeStreamEncoderFilter) AddEncodedData(buf types.IoBuffer, streamingFilter bool) {
	f.activeStream.addEncodedData(f, buf, streamingFilter)
}

func (f *activeStreamEncoderFilter) OnEncoderFilterAboveWriteBufferHighWatermark() {
	// todo
}

func (f *activeStreamEncoderFilter) OnEncoderFilterBelowWriteBufferLowWatermark() {
	// todo
}

func (f *activeStreamEncoderFilter) SetEncoderBufferLimit(limit uint32) {
	// todo
}

func (f *activeStreamEncoderFilter) EncoderBufferLimit() uint32 {
	// todo
	return 0
}
