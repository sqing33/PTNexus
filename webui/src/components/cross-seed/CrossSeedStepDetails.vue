<template>
  <div class="step-container details-container">
    <el-tabs v-model="activeTab" type="border-card" class="details-tabs">
      <el-tab-pane label="主要信息" name="main">
        <div class="main-info-container">
          <div class="full-width-form-column">
            <el-form label-position="top" class="fill-height-form">
              <div class="title-section">
                <el-form-item label="原始/待解析标题">
                  <el-input v-model="torrentData.original_main_title">
                    <template #append>
                      <el-button :icon="Refresh" @click="reparseTitle" :loading="isReparsing">
                        重新解析
                      </el-button>
                    </template>
                  </el-input>
                </el-form-item>
                <div class="title-components-grid">
                  <template v-if="filteredTitleComponents.length > 0">
                    <el-form-item
                      v-for="param in filteredTitleComponents"
                      :key="param.key"
                      :label="param.key"
                      :class="{
                        'unrecognized-section':
                          param.key === '制作组' &&
                          (!param.value || param.value.toUpperCase() === 'NOGROUP'),
                        'required-field-empty':
                          param.key === '年份' && (!param.value || param.value.trim() === ''),
                      }"
                    >
                      <el-input
                        v-model="param.value"
                        @input="(val: string) => handleTeamInput(param, val)"
                      />
                    </el-form-item>
                  </template>
                  <!-- 当没有解析出标题组件时，显示初始参数框 -->
                  <template v-else>
                    <el-form-item
                      v-for="(param, index) in initialTitleComponents"
                      :key="'init-' + index"
                      :label="param.key"
                      :class="{
                        'unrecognized-section':
                          param.key === '制作组' &&
                          (!param.value || param.value.toUpperCase() === 'NOGROUP'),
                        'required-field-empty':
                          param.key === '年份' && (!param.value || param.value.trim() === ''),
                      }"
                    >
                      <el-input
                        v-model="param.value"
                        @input="(val: string) => handleTeamInput(param, val)"
                      />
                    </el-form-item>
                  </template>
                </div>
              </div>

              <div class="bottom-info-section">
                <div class="subtitle-unrecognized-grid">
                  <!-- 副标题占4列 -->
                  <div class="subtitle-section" style="grid-column: span 4">
                    <el-form-item label="副标题">
                      <el-input v-model="torrentData.subtitle" />
                    </el-form-item>
                  </div>
                  <!-- 无法识别占1列 -->
                  <div
                    :class="{ 'unrecognized-section': unrecognizedValue }"
                    style="grid-column: span 1"
                  >
                    <el-form-item label="无法识别">
                      <el-input v-model="unrecognizedValue" />
                    </el-form-item>
                  </div>
                </div>

                <!-- 标准参数区域 -->
                <!-- [最终版本] 标准参数区域 -->
                <div class="standard-params-section">
                  <!-- 第一行：类型、媒介、视频编码、音频编码、分辨率 -->
                  <div class="standard-params-grid second-row">
                    <el-form-item label="类型 (type)">
                      <el-select
                        v-model="torrentData.standardized_params.type"
                        placeholder="请选择类型"
                        clearable
                        :class="{
                          'is-invalid': invalidStandardParams.includes('type'),
                          'is-empty': !torrentData.standardized_params.type,
                        }"
                        data-tag-style
                      >
                        <el-option
                          v-for="(label, value) in filteredTypeMappings"
                          :key="value"
                          :label="label"
                          :value="value"
                        />
                      </el-select>
                    </el-form-item>

                    <el-form-item label="媒介 (medium)">
                      <el-select
                        v-model="torrentData.standardized_params.medium"
                        placeholder="请选择媒介"
                        clearable
                        :class="{
                          'is-invalid': invalidStandardParams.includes('medium'),
                          'is-empty': !torrentData.standardized_params.medium,
                        }"
                        data-tag-style
                      >
                        <el-option
                          v-for="(label, value) in reverseMappings.medium"
                          :key="value"
                          :label="label"
                          :value="value"
                        />
                      </el-select>
                    </el-form-item>

                    <el-form-item label="视频编码 (video_codec)">
                      <el-select
                        v-model="torrentData.standardized_params.video_codec"
                        placeholder="请选择视频编码"
                        clearable
                        :class="{
                          'is-invalid': invalidStandardParams.includes('video_codec'),
                          'is-empty': !torrentData.standardized_params.video_codec,
                        }"
                        data-tag-style
                      >
                        <el-option
                          v-for="(label, value) in reverseMappings.video_codec"
                          :key="value"
                          :label="label"
                          :value="value"
                        />
                      </el-select>
                    </el-form-item>

                    <el-form-item label="音频编码 (audio_codec)">
                      <el-select
                        v-model="torrentData.standardized_params.audio_codec"
                        placeholder="请选择音频编码"
                        clearable
                        :class="{
                          'is-invalid': invalidStandardParams.includes('audio_codec'),
                          'is-empty': !torrentData.standardized_params.audio_codec,
                        }"
                        data-tag-style
                      >
                        <el-option
                          v-for="(label, value) in reverseMappings.audio_codec"
                          :key="value"
                          :label="label"
                          :value="value"
                        />
                      </el-select>
                    </el-form-item>

                    <el-form-item label="分辨率 (resolution)">
                      <el-select
                        v-model="torrentData.standardized_params.resolution"
                        placeholder="请选择分辨率"
                        clearable
                        :class="{
                          'is-invalid': invalidStandardParams.includes('resolution'),
                          'is-empty': !torrentData.standardized_params.resolution,
                        }"
                        data-tag-style
                      >
                        <el-option
                          v-for="(label, value) in reverseMappings.resolution"
                          :key="value"
                          :label="label"
                          :value="value"
                        />
                      </el-select>
                    </el-form-item>

                    <!-- 第二行：制作组、来源、标签 -->
                    <el-form-item label="制作组 (team)">
                      <el-select
                        v-model="torrentData.standardized_params.team"
                        placeholder="请选择制作组"
                        clearable
                        filterable
                        allow-create
                        default-first-option
                        :class="{
                          'is-invalid': invalidStandardParams.includes('team'),
                        }"
                        class="team-select"
                        data-tag-style
                      >
                        <el-option
                          v-for="(label, value) in reverseMappings.team"
                          :key="value"
                          :label="label"
                          :value="value"
                        />
                      </el-select>
                    </el-form-item>

                    <el-form-item label="来源 (source)">
                      <el-select
                        v-model="torrentData.standardized_params.source"
                        placeholder="请选择来源"
                        clearable
                        :class="{
                          'is-invalid': invalidStandardParams.includes('source'),
                        }"
                        data-tag-style
                      >
                        <el-option
                          v-for="(label, value) in reverseMappings.source"
                          :key="value"
                          :label="label"
                          :value="value"
                        />
                      </el-select>
                    </el-form-item>

                    <el-form-item class="tags-wide-item" label="标签 (tags)">
                      <el-select
                        v-model="torrentData.standardized_params.tags"
                        placeholder="请选择或输入标签"
                        multiple
                        filterable
                        allow-create
                        default-first-option
                        style="width: 100%"
                        :class="{
                          'is-invalid': invalidStandardParams.includes('tags'),
                        }"
                        data-tag-style
                      >
                        <template #tag="{ data }">
                          <el-tag
                            v-for="item in data"
                            :key="item.value"
                            :type="getTagType(item.value)"
                            :closable="true"
                            disable-transitions
                            @close="handleTagClose(item.value)"
                            style="margin: 2px"
                          >
                            <span>{{ reverseMappings.tags[item.value] || item.currentLabel }}</span>
                          </el-tag>
                        </template>
                        <el-option
                          v-for="option in allTagOptions"
                          :key="option.value"
                          :label="option.label"
                          :value="option.value"
                        >
                          <span
                            :style="{
                              color: invalidTagsList.includes(option.value) ? '#F56C6C' : '',
                            }"
                          >
                            {{ option.label }}
                          </span>
                        </el-option>
                      </el-select>
                    </el-form-item>
                  </div>
                </div>
              </div>
            </el-form>
          </div>
        </div>
      </el-tab-pane>

      <el-tab-pane label="海报与声明" name="poster-statement">
        <div class="poster-statement-container">
          <el-form label-position="top" class="fill-height-form">
            <div class="poster-statement-split">
              <div class="left-panel">
                <el-form-item label="声明" class="statement-item">
                  <el-input type="textarea" v-model="torrentData.intro.statement" :rows="18" />
                </el-form-item>
                <el-form-item>
                  <template #label>
                    <div class="form-label-with-button">
                      <span>海报链接</span>
                      <el-button
                        :icon="Refresh"
                        @click="refreshPosters"
                        :loading="isRefreshingPosters"
                        size="small"
                        type="text"
                      >
                        重新获取
                      </el-button>
                    </div>
                  </template>
                  <el-input type="textarea" v-model="torrentData.intro.poster" :rows="2" />
                </el-form-item>
              </div>
              <div class="right-panel">
                <div class="poster-preview-section">
                  <div class="preview-header">海报预览</div>
                  <div class="image-preview-container">
                    <template v-if="posterImages.length">
                      <img
                        v-for="(url, index) in posterImages"
                        :key="'poster-' + index"
                        :src="getProxyImageUrl(url)"
                        alt="海报预览"
                        class="preview-image"
                        @error="handleImageErrorWithProxy(url, 'poster', index)"
                      />
                    </template>
                    <div v-else class="preview-placeholder">暂无海报预览</div>
                  </div>
                </div>
              </div>
            </div>
          </el-form>
        </div>
      </el-tab-pane>

      <el-tab-pane label="视频截图" name="images">
        <div class="screenshot-container">
          <div class="form-column screenshot-text-column">
            <el-form label-position="top" class="fill-height-form">
              <el-form-item class="is-flexible">
                <template #label>
                  <div class="form-label-with-button">
                    <span>截图</span>
                    <div class="form-label-actions">
                      <el-tag v-if="isScreenshotReviewPending" type="warning" size="small">
                        待确认
                      </el-tag>
                      <el-button
                        v-if="isScreenshotReviewPending"
                        @click="confirmScreenshotReview"
                        :loading="isConfirmingScreenshotReview"
                        size="small"
                        type="primary"
                        plain
                      >
                        确认截图
                      </el-button>
                      <el-button
                        :icon="Refresh"
                        @click="refreshScreenshots"
                        :loading="isRefreshingScreenshots"
                        size="small"
                        type="text"
                      >
                        重新获取
                      </el-button>
                    </div>
                  </div>
                </template>
                <el-alert
                  v-if="isScreenshotReviewPending"
                  type="warning"
                  :closable="false"
                  show-icon
                  title="当前视频未检测到字幕流，请先确认截图是否截到了合适的中文字幕时间点。"
                  style="margin-bottom: 12px"
                />
                <el-input type="textarea" v-model="torrentData.intro.screenshots" :rows="20" />
              </el-form-item>
            </el-form>
          </div>
          <div class="preview-column screenshot-preview-column">
            <div class="carousel-container">
              <template v-if="screenshotImages.length">
                <el-carousel :interval="5000" height="500px" indicator-position="outside">
                  <el-carousel-item v-for="(url, index) in screenshotImages" :key="'ss-' + index">
                    <div class="carousel-image-wrapper">
                      <img
                        :src="getProxyImageUrl(url)"
                        alt="截图预览"
                        class="carousel-image"
                        @error="handleImageErrorWithProxy(url, 'screenshot', index)"
                      />
                    </div>
                  </el-carousel-item>
                </el-carousel>
              </template>
              <div v-else class="preview-placeholder">截图预览</div>
            </div>
          </div>
        </div>
      </el-tab-pane>
      <el-tab-pane label="简介详情" name="intro">
        <el-form label-position="top" class="fill-height-form">
          <el-form-item class="is-flexible">
            <template #label>
              <div class="form-label-with-button">
                <span>正文</span>
                <el-button
                  :icon="Refresh"
                  @click="refreshIntro"
                  :loading="isRefreshingIntro"
                  size="small"
                  type="text"
                >
                  重新获取
                </el-button>
              </div>
            </template>
            <el-input
              type="textarea"
              class="no-wrap-textarea"
              v-model="torrentData.intro.body"
              :rows="21"
            />
          </el-form-item>
          <el-row :gutter="20">
            <el-col :span="8">
              <el-form-item label="豆瓣链接">
                <el-input v-model="torrentData.douban_link" placeholder="请输入豆瓣电影链接" />
              </el-form-item>
            </el-col>
            <el-col :span="8">
              <el-form-item label="IMDb链接">
                <el-input v-model="torrentData.imdb_link" placeholder="请输入IMDb电影链接" />
              </el-form-item>
            </el-col>
            <el-col :span="8">
              <el-form-item label="TMDb链接">
                <el-input v-model="torrentData.tmdb_link" placeholder="请输入TMDb电影链接" />
              </el-form-item>
            </el-col>
          </el-row>
        </el-form>
      </el-tab-pane>
      <el-tab-pane label="媒体信息" name="mediainfo">
        <el-form label-position="top" class="fill-height-form">
          <el-form-item class="is-flexible">
            <template #label>
              <div class="form-label-with-button">
                <span>Mediainfo</span>
                <el-button
                  :icon="Refresh"
                  @click="refreshMediainfo"
                  :loading="isRefreshingMediainfo"
                  size="small"
                  type="text"
                >
                  重新获取
                </el-button>
              </div>
            </template>

            <div class="mediainfo-container">
              <!-- BDInfo 进度条 -->
              <div v-if="bdinfoProgress.visible" class="bdinfo-progress-inline">
                <el-card class="bdinfo-progress-card-inline" shadow="never">
                  <template #header>
                    <div class="progress-header">
                      <span>BDInfo 获取中...</span>
                      <div class="header-buttons">
                        <span class="background-hint">可在后台继续获取</span>
                        <el-button
                          :icon="Monitor"
                          @click="runInBackground"
                          size="small"
                          text
                          type="primary"
                        >
                          放置后台
                        </el-button>
                        <el-button
                          :icon="Close"
                          @click="stopBDInfoSSE"
                          size="small"
                          text
                          type="info"
                        >
                          取消获取
                        </el-button>
                      </div>
                    </div>
                  </template>
                  <el-progress
                    :percentage="bdinfoProgress.percent"
                    :status="bdinfoProgress.percent === 100 ? 'success' : ''"
                  />

                  <div class="progress-details-inline">
                    <div class="progress-info-row">
                      <div class="progress-item">原盘体积: {{ formatFileSize(discSize) }}</div>
                      <div class="progress-item">已用时: {{ bdinfoProgress.elapsedTime }}</div>
                      <div class="progress-item">剩余时间: {{ bdinfoProgress.remainingTime }}</div>
                    </div>
                  </div>
                </el-card>
              </div>

              <MediaInfoSummaryCard :text="torrentData.mediainfo" />

              <!-- Mediainfo 文本框 -->
              <el-input
                type="textarea"
                class="code-font no-wrap-textarea"
                v-model="torrentData.mediainfo"
                :rows="bdinfoProgress.visible ? 18 : 26"
              />
            </div>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane
        label="已过滤声明"
        name="filtered-declarations"
        class="filtered-declarations-pane"
      >
        <div class="filtered-declarations-container">
          <div class="filtered-declarations-header">
            <h3>已自动过滤的声明内容</h3>
            <el-tag type="warning" size="small">共 {{ filteredDeclarationsCount }} 条</el-tag>
          </div>
          <div class="filtered-declarations-content">
            <template v-if="filteredDeclarationsCount > 0">
              <div
                v-for="(declaration, index) in filteredDeclarationsList"
                :key="index"
                class="declaration-item"
              >
                <div class="declaration-header">
                  <span class="declaration-number">#{{ index + 1 }}</span>
                  <el-tag type="danger" size="small">已过滤</el-tag>
                </div>
                <pre class="declaration-content code-font">{{ declaration }}</pre>
              </div>
            </template>
            <div v-else class="no-filtered-declarations">
              <el-empty description="未检测到需要过滤的 ARDTU 声明内容" />
            </div>
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Close, Monitor, Refresh } from '@element-plus/icons-vue'
import { useCrossSeedPanelContext } from './crossSeedPanelContext'
import MediaInfoSummaryCard from './MediaInfoSummaryCard.vue'

const {
  activeTab,
  torrentData,
  filteredTitleComponents,
  initialTitleComponents,
  unrecognizedValue,
  reverseMappings,
  invalidStandardParams,
  reparseTitle,
  isReparsing,
  handleTeamInput,
  allTagOptions,
  invalidTagsList,
  isRestrictedTag,
  getTagType,
  handleTagClose,
  refreshPosters,
  isRefreshingPosters,
  posterImages,
  getProxyImageUrl,
  handleImageErrorWithProxy,
  refreshScreenshots,
  isRefreshingScreenshots,
  confirmScreenshotReview,
  isConfirmingScreenshotReview,
  isScreenshotReviewPending,
  screenshotImages,
  refreshIntro,
  isRefreshingIntro,
  refreshMediainfo,
  isRefreshingMediainfo,
  bdinfoProgress,
  discSize,
  formatFileSize,
  runInBackground,
  stopBDInfoSSE,
  filteredDeclarationsCount,
  filteredDeclarationsList,
} = useCrossSeedPanelContext()

const filteredTypeMappings = computed(() =>
  Object.fromEntries(
    Object.entries(reverseMappings.value.type).filter(([value]) => value !== 'category.animation'),
  ),
)
</script>
