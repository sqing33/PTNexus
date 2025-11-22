# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

PT Nexus is a PT (Private Tracker) torrent aggregation management platform that integrates downloader traffic statistics, seeding queries, multi-site torrent conversion, and local seeding file retrieval. The platform consists of three main services:

- **Frontend**: Vue.js web application (Element Plus UI)
- **Backend**: Flask-based Python API server
- **Batch Service**: Go service for batch processing operations

## Architecture

### Three-Tier Architecture
```
webui/          -> Vue.js frontend (port 5274 via updater proxy)
server/         -> Flask Python backend (port 5275)
batch/          -> Go batch processing service (port 5276)
updater/        -> Go updater service (port 5274)
```

### Core Backend Components

#### `/server/core/`
- **extractors/**: Site-specific torrent information extractors
  - `extractor.py`: Base extractor class and common functionality
  - `sites/`: Individual site implementations (hhanclub, ssd, keepfrds, etc.)
- **uploaders/**: Torrent upload handlers with fallback management
- **migrator.py**: Core torrent migration/conversion logic
- **services.py**: Main service orchestration and data tracking
- **iyuu.py**: IYUU API integration for cross-seeding

#### Key Architectural Pattern
The system uses a **Source Site → Standard Parameters → Target Site** three-layer architecture for torrent conversion to improve accuracy and avoid repeated parameter extraction.

### Database Support
- SQLite (default)
- MySQL
- PostgreSQL
Configurable via environment variables (`DB_TYPE`, `MYSQL_*`, `POSTGRES_*`)

## Development Commands

### Frontend (Vue.js)
```bash
cd webui
pnpm install          # Install dependencies
pnpm dev              # Development server
pnpm build            # Production build
pnpm type-check       # TypeScript type checking
pnpm lint             # Run linters (oxlint + eslint)
pnpm format           # Format code with prettier
```

### Backend (Python Flask)
```bash
cd server
python -m venv .venv                    # Create virtual environment
source .venv/bin/activate               # Activate virtual environment
pip install -r requirements.txt         # Install dependencies
python app.py                            # Run Flask server (port 5275)
```

### Batch Service (Go)
```bash
cd batch
go mod download                         # Download dependencies
go build .                               # Build batch service
./batch                                  # Run batch service (port 5276)
```

### Full Stack Development
```bash
# Start all services (development)
./start-services.sh                      # Starts all 3 services on respective ports

# Docker development
docker-compose up -d                    # Full stack with Docker
```

## Configuration

### Site Configuration
- **Site configs**: `server/configs/*.yaml` - Individual PT site configurations
- **Global mappings**: `server/configs/global_mappings.yaml` - Site parameter mappings
- **Sites data**: `server/sites_data.json` - Site metadata and capabilities

### Application Config
- **Main config**: `server/config.py` - Database, paths, and application settings
- **Environment**: Supports proxy settings via `http_proxy`/`https_proxy`

## Key Features & Components

### Torrent Conversion Pipeline
1. **Extraction**: Site-specific extractors pull torrent metadata from source sites
2. **Standardization**: Raw data converted to standard parameter format
3. **Upload**: Target site uploaders apply site-specific formatting and publish

### Site Support
The system supports numerous PT sites with individual extractor implementations:
- **hhanclub.py**: HhanClub site extractor
- **ssd.py**: SSD site extractor
- **keepfrds.py**: KeepFRDS site extractor
- And many more in `server/core/extractors/sites/`

### Batch Processing
The Go batch service handles:
- Bulk torrent operations
- Multi-site publishing with size restrictions (1GB limit for batch operations)
- Retry mechanisms with site priority fallback

## File Structure Conventions

### Python Backend
- **API endpoints**: `server/api/` - Flask route handlers
- **Models**: `server/models/` - Database models (e.g., `seed_parameter.py`)
- **Utilities**: `server/utils/` - Helper functions (media processing, douban integration, etc.)
- **Data**: `server/data/` - Runtime data storage and temporary files

### Frontend
- **Components**: `webui/src/components/` - Vue components
- **Views**: `webui/src/views/` - Page components
- **Stores**: `webui/src/stores/` - Pinia state management
- **Router**: `webui/src/router/` - Vue Router configuration

## Testing & Development Notes

### Database Migrations
The system includes automated database reconciliation and migration capabilities in `database.py` with `reconcile_historical_data()` function.

### Media Processing
- **MediaInfo integration**: `pymediainfo` for video metadata extraction
- **Image handling**: Custom image validation and processing utilities
- **Douban integration**: Movie metadata fetching from Douban API

### Proxy Support
All services respect system proxy settings and support per-site proxy configuration for PT site access.

## Hot Updates
The platform supports live hot updates via the updater service, automatically applying code updates from GitHub/Gitee repositories without requiring container rebuilds.