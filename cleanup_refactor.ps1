# Script para limpiar archivos duplicados después de la refactorización

Write-Host "Limpiando archivos duplicados..." -ForegroundColor Green

# Eliminar executor_new.go (duplicado de executor.go)
if (Test-Path "internal\executor\executor_new.go") {
    Remove-Item "internal\executor\executor_new.go" -Force
    Write-Host "✓ Eliminado executor_new.go" -ForegroundColor Yellow
}

# Eliminar interfaces.go (Result struct ahora está en executor.go)
if (Test-Path "internal\executor\interfaces.go") {
    Remove-Item "internal\executor\interfaces.go" -Force
    Write-Host "✓ Eliminado interfaces.go" -ForegroundColor Yellow
}

Write-Host "`nLimpiando caché de Go..." -ForegroundColor Green
go clean -cache
go clean -modcache  
go mod tidy

Write-Host "`nIntentando compilar..." -ForegroundColor Green
go build ./cmd/focusd

if ($LASTEXITCODE -eq 0) {
    Write-Host "`n✅ ¡Compilación exitosa!" -ForegroundColor Green
} else {
    Write-Host "`n❌ Errores de compilación. Verifica los mensajes arriba." -ForegroundColor Red
}
