<template>
  <div class="step-container publish-preview-container">
    <div class="publish-preview-content">
      <!-- 第一行：主标题 -->
      <div class="preview-row main-title-row">
        <div class="row-label">主标题：</div>
        <div class="row-content main-title-content">
          {{
            torrentData.final_publish_parameters?.['主标题 (预览)'] ||
            torrentData.original_main_title ||
            '暂无数据'
          }}
        </div>
      </div>

      <!-- 第二行：副标题 -->
      <div class="preview-row subtitle-row">
        <div class="row-label">副标题：</div>
        <div class="row-content subtitle-content">
          {{ torrentData.subtitle || '暂无数据' }}
        </div>
      </div>

      <!-- 资源信息（来自资源信息库命中） -->
      <div class="preview-row resource-info-row" v-if="torrentData.resource_info">
        <div class="row-label">资源信息：</div>
        <div class="row-content resource-info-content">
          <el-image
            v-if="torrentData.resource_info.poster_url"
            :src="getProxyImageUrl(torrentData.resource_info.poster_url)"
            :preview-src-list="[torrentData.resource_info.poster_url]"
            preview-teleported
            fit="cover"
            class="resource-info-poster"
          />
          <div class="resource-info-fields">
            <div class="resource-info-line">
              <span class="param-label">标题：</span>
              <span :class="['param-value', { empty: !torrentData.resource_info.title }]">
                {{ torrentData.resource_info.title || 'N/A' }}
              </span>
            </div>
            <div class="resource-info-line">
              <span class="param-label">年份：</span>
              <span :class="['param-value', { empty: !torrentData.resource_info.year }]">
                {{ torrentData.resource_info.year || 'N/A' }}
              </span>
              <span class="param-label" style="margin-left: 16px">国家：</span>
              <span :class="['param-value', { empty: !torrentData.resource_info.country }]">
                {{ torrentData.resource_info.country || 'N/A' }}
              </span>
            </div>
            <div class="resource-info-line">
              <span class="param-label">豆瓣ID：</span>
              <span :class="['param-value', { empty: !torrentData.resource_info.douban_id }]">
                {{ torrentData.resource_info.douban_id || 'N/A' }}
              </span>
              <span class="param-label" style="margin-left: 16px">IMDbID：</span>
              <span :class="['param-value', { empty: !torrentData.resource_info.imdb_id }]">
                {{ torrentData.resource_info.imdb_id || 'N/A' }}
              </span>
              <span class="param-label" style="margin-left: 16px">TMDbID：</span>
              <span :class="['param-value', { empty: !torrentData.resource_info.tmdb_id }]">
                {{ torrentData.resource_info.tmdb_id || 'N/A' }}
              </span>
            </div>
            <div class="resource-info-line resource-info-summary" v-if="torrentData.resource_info.summary">
              <span class="param-label">简介：</span>
              <span class="param-value">{{ torrentData.resource_info.summary }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- 第三行：媒介音频等各种参数 -->
      <div class="preview-row params-row">
        <div class="row-label">参数信息：</div>
        <div class="row-content">
          <!-- IMDb链接和标签在同一行 -->
          <div class="param-row">
            <div class="param-item imdb-item half-width">
              <div class="stacked-param-row">
                <div class="stacked-param-head">
                  <span class="param-label">豆瓣链接</span>
                  <span class="param-colon">：</span>
                </div>
                <div class="stacked-param-value-line">
                  <span
                    :class="[
                      'param-value',
                      'single-line-value',
                      { empty: !torrentData.douban_link || torrentData.douban_link === 'N/A' },
                    ]"
                  >
                    {{ torrentData.douban_link || 'N/A' }}
                  </span>
                </div>
              </div>
              <div class="stacked-param-row">
                <div class="stacked-param-head">
                  <span class="param-label">IMDb链接</span>
                  <span class="param-colon">：</span>
                </div>
                <div class="stacked-param-value-line">
                  <span
                    :class="[
                      'param-value',
                      'single-line-value',
                      { empty: !torrentData.imdb_link || torrentData.imdb_link === 'N/A' },
                    ]"
                  >
                    {{ torrentData.imdb_link || 'N/A' }}
                  </span>
                </div>
              </div>
              <div class="stacked-param-row">
                <div class="stacked-param-head">
                  <span class="param-label">TMDb链接</span>
                  <span class="param-colon">：</span>
                </div>
                <div class="stacked-param-value-line">
                  <span
                    :class="[
                      'param-value',
                      'single-line-value',
                      { empty: !torrentData.tmdb_link || torrentData.tmdb_link === 'N/A' },
                    ]"
                  >
                    {{ torrentData.tmdb_link || 'N/A' }}
                  </span>
                </div>
              </div>
            </div>
            <div class="param-item tags-item half-width">
              <div class="stacked-param-row">
                <div class="stacked-param-head">
                  <span class="param-label">标签</span>
                  <span class="param-colon">：</span>
                </div>
                <div class="stacked-param-value-line">
                  <div class="param-value-container stacked-tags-values">
                    <span
                      :class="[
                        'param-value',
                        'single-line-value',
                        { empty: !getMappedTags() || getMappedTags().length === 0 },
                      ]"
                    >
                      {{ getMappedTags().join(', ') || 'N/A' }}
                    </span>
                    <span class="param-standard-key" v-if="filteredTags && filteredTags.length > 0">
                      {{ filteredTags.join(', ') }}
                    </span>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- 其他参数在第二行开始排列 -->
          <div class="params-content">
            <div class="param-item inline-param">
              <span class="param-label">类型：</span>
              <div class="param-value-container">
                <span :class="['param-value', { empty: !getMappedValue('type') }]">
                  {{ getMappedValue('type') || 'N/A' }}
                </span>
                <span class="param-standard-key" v-if="torrentData.standardized_params.type">
                  {{ torrentData.standardized_params.type }}
                </span>
              </div>
            </div>
            <div class="param-item inline-param">
              <span class="param-label">媒介：</span>
              <div class="param-value-container">
                <span :class="['param-value', { empty: !getMappedValue('medium') }]">
                  {{ getMappedValue('medium') || 'N/A' }}
                </span>
                <span class="param-standard-key" v-if="torrentData.standardized_params.medium">
                  {{ torrentData.standardized_params.medium }}
                </span>
              </div>
            </div>
            <div class="param-item inline-param">
              <span class="param-label">视频编码：</span>
              <div class="param-value-container">
                <span :class="['param-value', { empty: !getMappedValue('video_codec') }]">
                  {{ getMappedValue('video_codec') || 'N/A' }}
                </span>
                <span class="param-standard-key" v-if="torrentData.standardized_params.video_codec">
                  {{ torrentData.standardized_params.video_codec }}
                </span>
              </div>
            </div>
            <div class="param-item inline-param">
              <span class="param-label">音频编码：</span>
              <div class="param-value-container">
                <span :class="['param-value', { empty: !getMappedValue('audio_codec') }]">
                  {{ getMappedValue('audio_codec') || 'N/A' }}
                </span>
                <span class="param-standard-key" v-if="torrentData.standardized_params.audio_codec">
                  {{ torrentData.standardized_params.audio_codec }}
                </span>
              </div>
            </div>
            <div class="param-item inline-param">
              <span class="param-label">分辨率：</span>
              <div class="param-value-container">
                <span :class="['param-value', { empty: !getMappedValue('resolution') }]">
                  {{ getMappedValue('resolution') || 'N/A' }}
                </span>
                <span class="param-standard-key" v-if="torrentData.standardized_params.resolution">
                  {{ torrentData.standardized_params.resolution }}
                </span>
              </div>
            </div>
            <div class="param-item inline-param">
              <span class="param-label">制作组：</span>
              <div class="param-value-container">
                <span :class="['param-value', { empty: !getMappedValue('team') }]">
                  {{ getMappedValue('team') || 'N/A' }}
                </span>
                <span class="param-standard-key" v-if="torrentData.standardized_params.team">
                  {{ torrentData.standardized_params.team }}
                </span>
              </div>
            </div>
            <div class="param-item inline-param">
              <span class="param-label">产地/来源：</span>
              <div class="param-value-container">
                <span :class="['param-value', { empty: !getMappedValue('source') }]">
                  {{ getMappedValue('source') || 'N/A' }}
                </span>
                <span class="param-standard-key" v-if="torrentData.standardized_params.source">
                  {{ torrentData.standardized_params.source }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 第四行：Mediainfo 可滚动区域 -->
      <div class="preview-row mediainfo-row">
        <div class="row-label">Mediainfo：</div>
        <div class="row-content mediainfo-content scrollable-content">
          <MediaInfoSummaryCard :text="torrentData.mediainfo" />
          <pre class="mediainfo-pre">{{ torrentData.mediainfo || '暂无数据' }}</pre>
        </div>
      </div>

      <!-- 第五行：声明+简介全部内容 -->
      <div class="preview-row description-row">
        <div class="row-label">简介内容：</div>
        <div class="row-content description-content">
          <!-- 声明内容 -->
          <div class="description-section">
            <div
              class="section-content no-wrap-preview-content"
              v-html="parseBBCode(torrentData.intro?.statement) || '暂无声明'"
            ></div>
          </div>

          <!-- 海报图片 -->
          <div class="description-section" v-if="posterImages.length > 0">
            <div class="image-gallery">
              <img
                v-for="(url, index) in posterImages"
                :key="'poster-preview-' + index"
                :src="getProxyImageUrl(url)"
                :alt="'海报 ' + (index + 1)"
                class="preview-image-inline"
                style="width: 300px"
                @error="handleImageErrorWithProxy(url, 'poster', index)"
              />
            </div>
          </div>

          <!-- 简介正文 -->
          <div class="description-section">
            <br />
            <div
              class="section-content no-wrap-preview-content"
              v-html="parseBBCode(torrentData.intro?.body) || '暂无正文'"
            ></div>
          </div>

          <!-- 视频截图 -->
          <div class="description-section" v-if="screenshotImages.length > 0">
            <div class="image-gallery">
              <img
                v-for="(url, index) in screenshotImages"
                :key="'screenshot-preview-' + index"
                :src="getProxyImageUrl(url)"
                :alt="'截图 ' + (index + 1)"
                class="preview-image-inline"
                @error="handleImageErrorWithProxy(url, 'screenshot', index)"
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useCrossSeedPanelContext } from './crossSeedPanelContext'
import MediaInfoSummaryCard from './MediaInfoSummaryCard.vue'

const {
  torrentData,
  getMappedValue,
  getMappedTags,
  filteredTags,
  parseBBCode,
  posterImages,
  screenshotImages,
  getProxyImageUrl,
  handleImageErrorWithProxy,
} = useCrossSeedPanelContext()
</script>

<style scoped>
.resource-info-content {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}

.resource-info-poster {
  width: 72px;
  height: 96px;
  border-radius: 4px;
  flex-shrink: 0;
}

.resource-info-fields {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.resource-info-line {
  line-height: 1.6;
}

.resource-info-summary .param-value {
  white-space: pre-wrap;
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 4;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
