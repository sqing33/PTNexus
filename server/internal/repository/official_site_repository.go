package repository

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

type siteForbiddenTransferRow struct {
	Nickname               string `gorm:"column:nickname"`
	Site                   string `gorm:"column:site"`
	ForbiddenTransferSites string `gorm:"column:forbidden_transfer_sites"`
}

// FindOfficialSiteByGroup 根据制作组名称解析官种站昵称。
// 参数/返回：releaseGroup 支持 team.*、@ 后缀和前导 -；命中返回站点 nickname，未命中返回空字符串。
// 失败场景：仓储未初始化或 sites 查询失败时返回错误。
// 副作用：无副作用，仅读取 sites 表。
func (r *MigrateRepository) FindOfficialSiteByGroup(releaseGroup string) (string, error) {
	return r.FindSiteNicknameByGroup(releaseGroup)
}

// IsTransferForbiddenByOfficialSite 判断官种站是否禁止转种到目标站。
// 参数/返回：officialSite 为官种站 nickname/site，targetSite 为目标站 nickname/site；命中返回 true 与匹配到的禁转站点。
// 失败场景：仓储未初始化或 sites 查询失败时返回错误。
// 副作用：无副作用，仅读取 sites 表。
func (r *MigrateRepository) IsTransferForbiddenByOfficialSite(officialSite, targetSite string) (bool, string, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return false, "", errors.New("migrate repo is nil")
	}
	officialSite = strings.TrimSpace(officialSite)
	targetSite = strings.TrimSpace(targetSite)
	if officialSite == "" || targetSite == "" {
		return false, "", nil
	}

	rows := make([]siteForbiddenTransferRow, 0)
	if err := r.store.DB.Table("sites").Select("nickname, site, forbidden_transfer_sites").Scan(&rows).Error; err != nil {
		return false, "", err
	}

	targetAliases := siteAliasesForName(rows, targetSite)
	if len(targetAliases) == 0 {
		targetAliases = map[string]struct{}{strings.ToLower(targetSite): {}}
	}

	for _, row := range rows {
		if !siteRowMatchesName(row, officialSite) {
			continue
		}
		for _, forbidden := range siteStringListFromAny(row.ForbiddenTransferSites) {
			if _, exists := targetAliases[strings.ToLower(strings.TrimSpace(forbidden))]; exists {
				return true, strings.TrimSpace(forbidden), nil
			}
		}
	}
	return false, "", nil
}

// BackfillOfficialSites 根据已有制作组字段回填 torrents 与 seed_parameters 的官种站。
// 参数/返回：无入参；返回数据库查询或更新错误。
// 失败场景：读取已有记录、匹配站点或更新表失败时返回错误。
// 副作用：写入 torrents.official_site 与 seed_parameters.official_site 的空值记录。
func (r *MigrateRepository) BackfillOfficialSites() error {
	if r == nil || r.store == nil || r.store.DB == nil {
		return errors.New("migrate repo is nil")
	}
	if err := r.backfillSeedParameterOfficialSites(); err != nil {
		return err
	}
	return r.backfillTorrentOfficialSites()
}

func (r *MigrateRepository) backfillSeedParameterOfficialSites() error {
	rows := make([]struct {
		Hash         string `gorm:"column:hash"`
		TorrentID    string `gorm:"column:torrent_id"`
		SiteName     string `gorm:"column:site_name"`
		Team         string `gorm:"column:team"`
		OfficialSite string `gorm:"column:official_site"`
	}, 0)
	if err := r.store.DB.Table("seed_parameters").Select("hash, torrent_id, site_name, team, official_site").Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if strings.TrimSpace(row.OfficialSite) != "" {
			continue
		}
		officialSite, err := r.FindOfficialSiteByGroup(row.Team)
		if err != nil {
			return err
		}
		if strings.TrimSpace(officialSite) == "" {
			continue
		}
		if err := r.store.DB.Table("seed_parameters").Where("hash = ? AND torrent_id = ? AND site_name = ?", row.Hash, row.TorrentID, row.SiteName).Update("official_site", officialSite).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *MigrateRepository) backfillTorrentOfficialSites() error {
	rows := make([]struct {
		Hash         string `gorm:"column:hash"`
		DownloaderID string `gorm:"column:downloader_id"`
		TorrentGroup string `gorm:"column:torrent_group"`
		OfficialSite string `gorm:"column:official_site"`
	}, 0)
	query := "hash, downloader_id, " + r.store.GroupColumn() + " AS torrent_group, official_site"
	if err := r.store.DB.Table("torrents").Select(query).Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if strings.TrimSpace(row.OfficialSite) != "" {
			continue
		}
		officialSite, err := r.FindOfficialSiteByGroup(row.TorrentGroup)
		if err != nil {
			return err
		}
		if strings.TrimSpace(officialSite) == "" {
			continue
		}
		if err := r.store.DB.Table("torrents").Where("hash = ? AND downloader_id = ?", row.Hash, row.DownloaderID).Update("official_site", officialSite).Error; err != nil {
			return err
		}
	}
	return nil
}

func updateTorrentOfficialSiteFromSeedRecord(tx *gorm.DB, record map[string]any) error {
	officialSite := strings.TrimSpace(toString(record["official_site"], ""))
	if tx == nil || officialSite == "" {
		return nil
	}
	updates := map[string]any{"official_site": officialSite}
	if hash := strings.TrimSpace(toString(record["hash"], "")); hash != "" {
		if err := tx.Table("torrents").Where("LOWER(TRIM(hash)) = LOWER(TRIM(?))", hash).Updates(updates).Error; err != nil {
			return err
		}
	}
	if name := strings.TrimSpace(toString(record["name"], "")); name != "" {
		return tx.Table("torrents").Where("LOWER(TRIM(name)) = LOWER(TRIM(?))", name).Updates(updates).Error
	}
	return nil
}

func siteAliasesForName(rows []siteForbiddenTransferRow, name string) map[string]struct{} {
	aliases := map[string]struct{}{}
	for _, row := range rows {
		if !siteRowMatchesName(row, name) {
			continue
		}
		if nickname := strings.ToLower(strings.TrimSpace(row.Nickname)); nickname != "" {
			aliases[nickname] = struct{}{}
		}
		if site := strings.ToLower(strings.TrimSpace(row.Site)); site != "" {
			aliases[site] = struct{}{}
		}
	}
	return aliases
}

func siteRowMatchesName(row siteForbiddenTransferRow, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(row.Nickname), name) || strings.EqualFold(strings.TrimSpace(row.Site), name)
}
