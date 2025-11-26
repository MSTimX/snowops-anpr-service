# Скрипт для тестирования webhook от камеры Hikvision

$xmlContent = @"
<?xml version='1.0' encoding='UTF-8'?>
<EventNotificationAlert version='2.0'>
    <ipAddress>194.26.239.249</ipAddress>
    <portNo>80</portNo>
    <protocolType>HTTP</protocolType>
    <eventType>ANPR</eventType>
    <dateTime>2025-11-26T12:00:00Z</dateTime>
    <channelID>1</channelID>
    <deviceID>DS-TCG406-E</deviceID>
    <ANPR>
        <licensePlate>795AAZ15</licensePlate>
        <confidenceLevel>95.5</confidenceLevel>
        <vehicleType>truck</vehicleType>
        <color>orange</color>
        <direction>forward</direction>
        <laneNo>1</laneNo>
    </ANPR>
    <picInfo>
        <ftpPath>http://194.26.239.249/snapshot.jpg</ftpPath>
    </picInfo>
</EventNotificationAlert>
"@

# Сохраняем XML во временный файл
$tempFile = [System.IO.Path]::GetTempFileName() + ".xml"
$xmlContent | Out-File -FilePath $tempFile -Encoding UTF8

Write-Host "Отправка тестового запроса на http://localhost:8080/api/v1/anpr/hikvision"
Write-Host "Номер: 795AAZ15"
Write-Host ""

try {
    # Используем curl для отправки multipart/form-data
    $boundary = [System.Guid]::NewGuid().ToString()
    $bodyLines = @(
        "--$boundary",
        "Content-Disposition: form-data; name=`"xml`"; filename=`"event.xml`"",
        "Content-Type: application/xml",
        "",
        $xmlContent,
        "--$boundary--"
    )
    $body = $bodyLines -join "`r`n"
    
    $response = Invoke-WebRequest -Uri "http://localhost:8080/api/v1/anpr/hikvision" `
        -Method POST `
        -ContentType "multipart/form-data; boundary=$boundary" `
        -Body ([System.Text.Encoding]::UTF8.GetBytes($body))
    
    Write-Host "Успешно! Статус: $($response.StatusCode)"
    Write-Host "Ответ: $($response.Content)"
} catch {
    Write-Host "Ошибка: $($_.Exception.Message)"
    if ($_.Exception.Response) {
        $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
        $responseBody = $reader.ReadToEnd()
        Write-Host "Тело ответа: $responseBody"
    }
} finally {
    # Удаляем временный файл
    if (Test-Path $tempFile) {
        Remove-Item $tempFile -Force
    }
}

