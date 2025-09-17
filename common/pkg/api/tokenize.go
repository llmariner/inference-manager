package api

// ConvertTokenizeRequestToProto converts a tokenize request to the protobuf format.
func ConvertTokenizeRequestToProto(body []byte) ([]byte, error) {
	fs := []convertF{
		convertContentStringToArray,
	}
	return applyConvertFuncs(body, fs)
}

// ConvertTokenizeRequestToOpenAI converts the request to the OpenAI format.
func ConvertTokenizeRequestToOpenAI(body []byte) ([]byte, error) {
	fs := []convertF{
		// The order of the functions is the opposite of the ConvertTokenizeRequestToProto.
		//
		// We don't have a function that corresponds to convertContentStringToArray as the conversion
		// doesn't break the OpenAI API spec.
	}
	return applyConvertFuncs(body, fs)
}
