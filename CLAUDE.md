# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

PT Nexus is a sophisticated PT (Private Tracker) torrent management platform with a multi-service architecture. It enables torrent transfers between different private trackers through automated parameter conversion and API integration, featuring download traffic statistics, seed querying, multi-site torrent transferring, and local seed file management.

## Architecture

### Multi-Service Architecture
- **Frontend**: Vue 3 + TypeScript + Element Plus (port 5274 via proxy)
- **Backend API**: Python Flask (port 5275)
- **Batch Service**: Go service for batch operations (port 5276)
- **Updater Service**: Go service for hot updates (port 5274)
- **Box Proxy**: Standalone Go proxy service for remote box access

### Directory Structure
```
/home/sqing/Codes/Docker.pt-nexus-dev/
├── webui/           # Vue.js frontend application
├── server/          # Python Flask backend API
├── batch/           # Go batch processing service
├── updater/         # Go hot update service
├── proxy/           # Go box proxy service
├── bdinfo/          # BDInfo tools for Blu-ray analysis
├── wiki/            # Documentation files
└── start-services.sh # Multi-service startup script
```

## Development Commands

### Frontend Development (Vue.js)
```bash
cd /home/sqing/Codes/Docker.pt-nexus-dev/webui
pnpm install          # Install dependencies
pnpm dev              # Start development server (port 5173)
pnpm build            # Build for production
pnpm type-check       # TypeScript type checking
pnpm lint             # Run linting (oxlint + eslint)
pnpm format           # Format code with prettier
```

### Backend Development (Python Flask)
```bash
cd /home/sqing/Codes/Docker.pt-nexus-dev/server
python app.py         # Start Flask server (port 5275)
```

### Go Services Development
```bash
# Batch service
cd /home/sqing/Codes/Docker.pt-nexus-dev/batch
go run batch.go       # Run batch service (port 5276)
go build -o batch batch.go  # Build binary

# Updater service
cd /home/sqing/Codes/Docker.pt-nexus-dev/updater
go run updater.go     # Run updater service (port 5274)
go build -o updater updater.go  # Build binary

# Box proxy service
cd /home/sqing/Codes/Docker.pt-nexus-dev/proxy
go run proxy.go       # Run proxy service
go build -o pt-nexus-box-proxy proxy.go  # Build binary
```

### Full Application Startup
```bash
cd /home/sqing/Codes/Docker.pt-nexus-dev
./start-services.sh   # Start all services in production mode
```

## Configuration

### Environment Variables
Development configuration is in `/home/sqing/Codes/Docker.pt-nexus-dev/server/.env`:

```bash
# Database configuration
DB_TYPE=mysql                    # sqlite, mysql, or postgresql
MYSQL_HOST=192.168.1.100
MYSQL_PORT=3306
MYSQL_DATABASE=pt-nexus-test
MYSQL_USER=root
MYSQL_PASSWORD=qaqaa123

# Development settings
DEV_ENV=true                     # Enables development paths
UPLOAD_TEST_MODE=true           # Test upload mode
SCREENSHOTS=false               # Disable screenshots
TZ=Asia/Shanghai                # Timezone
```

### Key Configuration Files
- `/home/sqing/Codes/Docker.pt-nexus-dev/server/config.py` - Main configuration manager
- `/home/sqing/Codes/Docker.pt-nexus-dev/server/sites_data.json` - PT sites configuration
- `/home/sqing/Codes/Docker.pt-nexus-dev/server/configs/global_mappings.yaml` - Global torrent mappings
- `/home/sqing/Codes/Docker.pt-nexus-dev/CHANGELOG.json` - Version and changelog data

## Database Setup

The application supports three database types:
- **SQLite**: Default for simple deployments (file-based)
- **MySQL**: For production environments
- **PostgreSQL**: Alternative for production environments

Database configuration is handled automatically based on `DB_TYPE` environment variable.

## API Structure

The Flask backend provides RESTful APIs organized by functionality:
- `routes_auth.py` - Authentication and user management
- `routes_config.py` - Configuration management
- `routes_cross_seed_data.py` - Cross-seeding data operations
- `routes_local_query.py` - Local torrent queries
- `routes_management.py` - General management operations
- `routes_stats.py` - Statistics and traffic data
- `routes_torrents.py` - Torrent management
- `routes_torrent_transfer.py` - Torrent transfer operations
- `routes_sites.py` - PT site management
- `routes_migrate.py` - Data migration operations

## Frontend Architecture

The Vue.js frontend uses:
- **Vue Router** for navigation
- **Pinia** for state management
- **Element Plus** for UI components
- **ECharts** for data visualization
- **Axios** for API communication
- **TypeScript** for type safety

Key directories:
- `src/views/` - Page components
- `src/components/` - Reusable components
- `src/stores/` - Pinia state stores
- `src/types/` - TypeScript type definitions
- `src/router/` - Vue Router configuration

## Docker Deployment

### Build Process
The multi-stage Dockerfile builds:
1. Frontend assets (Node.js + pnpm)
2. Go batch service
3. Go updater service
4. Final Python runtime with all components

### Environment Configuration
Key Docker environment variables:
- `PORT=5274` - Main application port
- `DB_TYPE` - Database type selection
- Database connection variables (MySQL/PostgreSQL)
- `TZ` - Timezone setting
- Proxy settings (`http_proxy`, `https_proxy`)

### Volume Mounts
- `/app/data` - Application data directory
- `/pt` - Video files storage
- `/data/tr_torrents/tr*` - Transmission torrent directories

## Development Workflow

### Hot Updates
The application supports hot updates through the updater service. Updates are pulled from GitHub and applied automatically without container restart.

### Testing
- Frontend: Use `pnpm type-check` and `pnpm lint`
- Backend: Python logging is configured for development
- Integration: Test with Docker Compose setup

### Logging
- Python: Configured with detailed logging including process IDs
- Development: Check console output and log files
- Production: Container logs provide service status

## Special Features

### Torrent Transfer System
- Source site → Standard parameters → Target site architecture
- Database caching to avoid repeated parameter extraction
- Support for multiple PT sites with custom mappings
- Batch transfer capabilities with size restrictions (1GB limit for batch operations)

### Box Proxy Service
- Standalone Go service for remote box access
- Handles MediaInfo extraction, screenshots, and file operations
- Resolves network latency and remote access issues
- Multi-platform support (Linux AMD64/ARM64)

### Traffic Statistics
- Real-time and historical traffic monitoring
- Multi-downloader support (qBittorrent, Transmission)
- Interactive charts with ECharts
- Configurable time ranges and data aggregation

## Key Dependencies

### Python Backend
- Flask + Flask-CORS for web framework
- qbittorrent-api, transmission-rpc for downloader integration
- mysql-connector-python, psycopg2-binary for database support
- beautifulsoup4, cloudscraper for web scraping
- PyYAML for configuration management

### Frontend
- Vue 3 ecosystem with TypeScript
- Element Plus for UI components
- ECharts for data visualization
- Axios for HTTP requests

### Go Services
- Standard library only for batch and updater services
- Additional dependencies for proxy service (check proxy/go.mod)

## Security Notes

- Authentication system with password hashing
- CookieCloud integration for secure cookie management
- Proxy settings for network access
- Environment-based configuration for sensitive data