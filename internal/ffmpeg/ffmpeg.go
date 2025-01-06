package ffmpeg

/*
#cgo pkg-config: libavcodec libavformat libavutil
#include <libavformat/avformat.h>
#include <libavutil/avutil.h>
#include <libavutil/dict.h>
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"unsafe"
)

func JoinFiles(input, output string) error {
	var ret C.int

	//C.av_log_set_level(C.AV_LOG_VERBOSE)

	var fmtCtx *C.AVFormatContext = nil
	defer C.avformat_free_context(fmtCtx)

	getInStream := func(i C.int) *C.struct_AVStream {
		return *(**C.struct_AVStream)(unsafe.Pointer(uintptr(unsafe.Pointer(fmtCtx.streams)) + uintptr(i)*unsafe.Sizeof(*fmtCtx.streams)))
	}

	var inputFilename *C.char = C.CString(input)
	defer C.free(unsafe.Pointer(inputFilename))

	var inputFormat *C.AVInputFormat = C.av_find_input_format(C.CString("concat"))

	var options *C.struct_AVDictionary = nil
	defer C.av_dict_free(&options)

	// Convert Go strings to C strings
	var safeKey *C.char = C.CString("safe")
	defer C.free(unsafe.Pointer(safeKey))

	// Call av_dict_set
	ret = C.av_dict_set_int(&options, safeKey, 0, 0)

	if ret = C.avformat_open_input(&fmtCtx, inputFilename, inputFormat, &options); ret < 0 {
		return fmt.Errorf("could not open input file, %d", int(ret))
	}
	defer C.avformat_close_input(&fmtCtx)

	var outputCtx *C.AVFormatContext = nil

	getOutStream := func(i C.int) *C.struct_AVStream {
		return *(**C.struct_AVStream)(unsafe.Pointer(uintptr(unsafe.Pointer(outputCtx.streams)) + uintptr(i)*unsafe.Sizeof(*outputCtx.streams)))
	}

	var outputFilename *C.char = C.CString(output)

	C.avformat_alloc_output_context2(&outputCtx, nil, nil, outputFilename)
	if outputCtx == nil {
		return fmt.Errorf("could not create output context")
	}

	for i := 0; i < int(fmtCtx.nb_streams); i++ {
		outStream := C.avformat_new_stream(outputCtx, nil)
		if outStream == nil {
			return fmt.Errorf("could not allocate outStream")
		}
		inStream := getInStream(C.int(i))
		if ret = C.avcodec_parameters_copy(outStream.codecpar, inStream.codecpar); ret < 0 {
			return fmt.Errorf("could not copy codec parameters, %d", int(ret))
		}
		outStream.codecpar.codec_tag = 0
	}

	if ret = C.avio_open(&outputCtx.pb, outputFilename, C.AVIO_FLAG_WRITE); ret < 0 {
		return fmt.Errorf("could not open output file, %d", int(ret))
	}

	if ret = C.avformat_write_header(outputCtx, nil); ret < 0 {
		return fmt.Errorf("could not write header, %d", int(ret))
	}

	var pkt C.AVPacket
	for {
		if ret = C.av_read_frame(fmtCtx, &pkt); ret < 0 {
			C.av_packet_unref(&pkt)
			break
		}

		inStream := getInStream(pkt.stream_index)
		outStream := getOutStream(pkt.stream_index)

		pkt.pts = C.av_rescale_q_rnd(pkt.pts, inStream.time_base, outStream.time_base, C.AV_ROUND_NEAR_INF|C.AV_ROUND_PASS_MINMAX)
		pkt.dts = C.av_rescale_q_rnd(pkt.dts, inStream.time_base, outStream.time_base, C.AV_ROUND_NEAR_INF|C.AV_ROUND_PASS_MINMAX)
		pkt.duration = C.av_rescale_q(pkt.duration, inStream.time_base, outStream.time_base)
		pkt.pos = -1

		if ret = C.av_interleaved_write_frame(outputCtx, &pkt); ret < 0 {
			return fmt.Errorf("could not write frame, %d", int(ret))
		}
		C.av_packet_unref(&pkt)
	}

	if ret = C.av_write_trailer(outputCtx); ret < 0 {
		return fmt.Errorf("could not write trailer, %d", int(ret))
	}

	return nil
}
