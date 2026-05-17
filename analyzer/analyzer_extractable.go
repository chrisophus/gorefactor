package analyzer

// Convert maps to slices

func isBlockExtractable(info *BlockInfo, config *ExtractionConfig) bool {
	if config == nil {
		config = DefaultConfig()
	}

	// Check complexity bounds
	if info.Complexity < config.MinComplexity || info.Complexity > config.MaxComplexity {
		return false
	}

	// Check variable counts
	if len(info.ReadVars) > config.MaxReadVars || len(info.WriteVars) > config.MaxWriteVars {
		return false
	}

	// Check statement count
	if info.StatementCount < config.MinStatements || info.StatementCount > config.MaxStatements {
		return false
	}

	// Check if all read variables are either written to in the block
	// or should be passed as parameters
	for _, readVar := range info.ReadVars {
		isWritten := false
		for _, writeVar := range info.WriteVars {
			if readVar == writeVar {
				isWritten = true
				break
			}
		}
		if !isWritten {
			// This variable is read but not written to in the block
			// It needs to be passed as a parameter
			info.Variables = append(info.Variables, readVar)
		}
	}

	return true
}
