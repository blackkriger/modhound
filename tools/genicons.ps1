# Writes build/appicon.png, build/windows/icon.ico and build/modhound.png; run from the repo root: pwsh ./tools/genicons.ps1
param([string]$OutDir = "build", [string]$PreviewDir = "")
Add-Type -AssemblyName System.Drawing

function New-Image([string]$text, [int]$size, [int]$height = 0) {
  $path = New-Object System.Drawing.Drawing2D.GraphicsPath
  $ff = New-Object System.Drawing.FontFamily('Times New Roman')
  $path.AddString($text, $ff, [int][System.Drawing.FontStyle]::Regular, [float]100, (New-Object System.Drawing.PointF(0, 0)), [System.Drawing.StringFormat]::GenericTypographic)
  $b = $path.GetBounds()
  if ($height -gt 0) {
    $penW = [float]($height * 0.07)
    $k = [float](($height - 2 * $penW) / $b.Height)
    $w = [int][Math]::Ceiling($b.Width * $k + 2 * $penW)
    $h = $height
  } else {
    $penW = [float]([Math]::Max(1.2, $size * 0.07))
    $k = [float](($size - 2 * $penW) / [Math]::Max($b.Width, $b.Height))
    $w = $size
    $h = $size
  }
  $bmp = New-Object System.Drawing.Bitmap($w, $h)
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
  $g.Clear([System.Drawing.Color]::Transparent)
  $m = New-Object System.Drawing.Drawing2D.Matrix
  $m.Translate([float](($w - $b.Width * $k) / 2), [float](($h - $b.Height * $k) / 2))
  $m.Scale($k, $k)
  $m.Translate(-$b.X, -$b.Y)
  $path.Transform($m)
  $m.Dispose()

  $pen = New-Object System.Drawing.Pen([System.Drawing.Color]::Black, $penW)
  $pen.LineJoin = [System.Drawing.Drawing2D.LineJoin]::Round
  $g.DrawPath($pen, $path)
  $g.FillPath([System.Drawing.Brushes]::White, $path)

  $pen.Dispose(); $path.Dispose(); $g.Dispose()
  $ms = New-Object System.IO.MemoryStream
  $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
  $bmp.Dispose()
  return , $ms.ToArray()
}

function New-Frame([int]$size) {
  return , (New-Image 'mh' $size)
}

function Save-Ico([string]$path) {
  $sizes = 16, 24, 32, 48, 256
  $frames = @{}
  foreach ($s in $sizes) { $frames[$s] = (New-Frame $s) }
  $out = New-Object System.IO.MemoryStream
  $bw = New-Object System.IO.BinaryWriter($out)
  $bw.Write([uint16]0); $bw.Write([uint16]1); $bw.Write([uint16]$sizes.Count)
  $offset = 6 + 16 * $sizes.Count
  foreach ($s in $sizes) {
    $len = $frames[$s].Length
    $bw.Write([byte]($s % 256)); $bw.Write([byte]($s % 256)); $bw.Write([byte]0); $bw.Write([byte]0)
    $bw.Write([uint16]1); $bw.Write([uint16]32); $bw.Write([uint32]$len); $bw.Write([uint32]$offset)
    $offset += $len
  }
  foreach ($s in $sizes) { $bw.Write($frames[$s]) }
  $bw.Flush()
  [System.IO.File]::WriteAllBytes($path, $out.ToArray())
  $bw.Dispose()
  Write-Output ("{0}: {1} bytes" -f (Split-Path $path -Leaf), (Get-Item $path).Length)
}

New-Item -ItemType Directory -Force (Join-Path $OutDir 'windows') | Out-Null
[System.IO.File]::WriteAllBytes((Join-Path $OutDir 'appicon.png'), (New-Frame 512))
Save-Ico (Join-Path $OutDir 'windows/icon.ico')
[System.IO.File]::WriteAllBytes((Join-Path $OutDir 'modhound.png'), (New-Image 'modhound' 0 96))
Write-Output "modhound.png written"

if ($PreviewDir) {
  foreach ($s in 16, 32, 72) {
    [System.IO.File]::WriteAllBytes((Join-Path $PreviewDir ("prev_" + $s + ".png")), (New-Frame $s))
  }
  Write-Output "previews written"
}
