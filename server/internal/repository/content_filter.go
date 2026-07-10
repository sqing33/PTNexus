package repository

import "fmt"

const videoSeedTypesSQL = "'category.movie', 'category.tv_series', 'category.animation', 'category.documentaries', 'category.tv_shows'"

func videoSeedTypeCondition(field string) string {
	return fmt.Sprintf("%s IN (%s)", field, videoSeedTypesSQL)
}

func videoTorrentExistsCondition(torrentAlias string) string {
	return fmt.Sprintf(`EXISTS (
			SELECT 1 FROM seed_parameters sp_video
			WHERE sp_video.hash = %s.hash
			  AND %s
		)`, torrentAlias, videoSeedTypeCondition("sp_video.type"))
}
