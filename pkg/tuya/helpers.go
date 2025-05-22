package tuya

func GetStreamType(skill *Skill, streamResolution string) int {
	// Default streamType if nothing is found
	defaultStreamType := 1

	if skill == nil || len(skill.Videos) == 0 {
		return defaultStreamType
	}

	// Find the highest and lowest resolution
	var highestResType = defaultStreamType
	var highestRes = 0
	var lowestResType = defaultStreamType
	var lowestRes = 0

	for _, video := range skill.Videos {
		res := video.Width * video.Height

		// Highest Resolution
		if res > highestRes {
			highestRes = res
			highestResType = video.StreamType
		}

		// Lower Resolution (or first if not set yet)
		if lowestRes == 0 || res < lowestRes {
			lowestRes = res
			lowestResType = video.StreamType
		}
	}

	// Return the streamType based on the selection
	switch streamResolution {
	case "hd":
		return highestResType
	case "sd":
		return lowestResType
	default:
		return defaultStreamType
	}
}

func IsHEVC(skill *Skill, streamType int) bool {
	for _, video := range skill.Videos {
		if video.StreamType == streamType {
			return video.CodecType == 4
		}
	}

	return false
}
