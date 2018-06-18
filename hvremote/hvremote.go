package hvremote

import (
	"errors"
	"io"
	"strconv"
	"strings"
	"github.com/flynnhandley/psremote"
)

type HypervRemote struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Ps      *psremote.PSRemote
	Session string
}

func NewHypervRemote(userName, password, computerName string, useSSL bool) (*HypervRemote, error) {
	ps, _ := psremote.NewPSRemote(userName, password, computerName, useSSL)
	hvremote := &HypervRemote{
		Session: `$secpasswd = ConvertTo-SecureString "` + password + `" -AsPlainText -Force
	$creds = New-Object System.Management.Automation.PSCredential ("` + userName + `", $secpasswd)
	$Session = New-PsSession -Computername "` + computerName + `" -credential $creds
	`,
		Ps: ps,
	}

	return hvremote, nil
}

func (hvc *HypervRemote) InvokeCommand(scriptBlock string, params map[string]string) (string, error) {

	cmdOut, err := hvc.Ps.OutputWinRm(scriptBlock, params)
	return cmdOut, err
}

func (hvc *HypervRemote) TestConnectivity() error {
	_, err := hvc.Ps.OutputWinRm("", nil)
	return err
}

// PutFile sends a file to a remote host via pssession
func (hvc *HypervRemote) PutFile(source, dest string) error {

	var script = hvc.Session + `Copy-Item -Path ` + source + ` -Destination ` + dest + ` -ToSession $Session`

	params := map[string]string{"source": source, "dest": dest}
	_, err := hvc.Ps.Output(script, params)
	return err
}

// GetFile returns a file from a remote host via pssession
func (hvc *HypervRemote) GetFile(source, dest string) error {

	var script = ``
	params := map[string]string{"source": source, "dest": dest}
	_, err := hvc.Ps.Output(script, params)
	return err
}

func (hvc *HypervRemote) Hash(path, algorithm string) (string, error) {
	var script = `
$path = $using:path
$algorithm = $using:algorithm

if(!(Test-Path $path)){Write-Error "Cannot find file: $path"}

return (Get-FileHash -Path $Path -Algorith $algorithm).hash
`

	params := map[string]string{"path": path, "algorithm": algorithm}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)

	return cmdOut, err
}

func (hvc *HypervRemote) Download(source, dest, hash, algorithm string) (string, error) {
	var script = `
	$source = $using:source
	$dest = $using:dest

	(New-Object System.Net.WebClient).DownloadFile($Source, $Dest)
`
	params := map[string]string{"source": source, "dest": dest, "hash": hash, "algorithm": algorithm}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)

	return cmdOut, err
}

func (hvc *HypervRemote) GetHostAdapterIpAddressForSwitch(switchName string) (string, error) {
	var script = `
$switchName = $using:switchName
$HostVMAdapter = Get-VMNetworkAdapter -ManagementOS -SwitchName $switchName
if ($HostVMAdapter){
    $HostNetAdapter = Get-NetAdapter | ?{ $_.DeviceID -eq $HostVMAdapter.DeviceId }
    if ($HostNetAdapter){
        $HostNetAdapterConfiguration =  @(get-wmiobject win32_networkadapterconfiguration -filter "IPEnabled = 'TRUE' AND InterfaceIndex=$($HostNetAdapter.ifIndex)")
        if ($HostNetAdapterConfiguration){
            return @($HostNetAdapterConfiguration.IpAddress)[0]
        }
    }
}
return $false
`

	params := map[string]string{"switchName": switchName}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)

	return cmdOut, err
}

func (hvc *HypervRemote) GetVirtualMachineNetworkAdapterAddress(vmName, adapterName string) (string, error) {

	var script = `
$vmName = $using:vmName
$adapterName = $using:adapterName
$addressIndex = $using:addressIndex
try {
	Start-Sleep 20
  $adapter = Get-VMNetworkAdapter -VMName $vmName -Name "$adapterName"
  $ip = $adapter.IPAddresses[$addressIndex]
  if($ip -eq $null) {
    return
  }
} catch {
  return
}
$ip
`

	params := map[string]string{"vmName": vmName, "adapterName": adapterName, "addressIndex": "0"}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)

	return cmdOut, err
}

func (hvc *HypervRemote) CreateDvdDrive(vmName string, isoPath string, generation uint) (uint, uint, error) {

	var script = `
$vmName = $using:vmName
$isoPath = $using:isoPath
$dvdController = Add-VMDvdDrive -VMName $vmName -path $isoPath -Passthru
$dvdController | Set-VMDvdDrive -path $null
$result = "$($dvdController.ControllerNumber),$($dvdController.ControllerLocation)"
$result
`

	params := map[string]string{"vmName": vmName, "isoPath": isoPath}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)

	if err != nil {
		return 0, 0, err
	}

	cmdOutArray := strings.Split(cmdOut, ",")
	if len(cmdOutArray) != 2 {
		return 0, 0, errors.New("Did not return controller number and controller location")
	}

	controllerNumberTemp, err := strconv.ParseUint(strings.TrimSpace(cmdOutArray[0]), 10, 64)
	if err != nil {
		return 0, 0, err
	}
	controllerNumber := uint(controllerNumberTemp)

	controllerLocationTemp, err := strconv.ParseUint(strings.TrimSpace(cmdOutArray[1]), 10, 64)
	if err != nil {
		return controllerNumber, 0, err
	}
	controllerLocation := uint(controllerLocationTemp)

	return controllerNumber, controllerLocation, err
}

func (hvc *HypervRemote) MountDvdDrive(vmName string, path string, controllerNumber uint, controllerLocation uint) error {

	var script = `
$vmName = $using:vmName
$path = $using:path
$controllerNumber = $using:controllerNumber
$controllerLocation = $using:controllerLocation

$vmDvdDrive = Get-VMDvdDrive -VMName $vmName -ControllerNumber $controllerNumber -ControllerLocation $controllerLocation
if (!$vmDvdDrive) {throw 'unable to find dvd drive'}
Set-VMDvdDrive -VMName $vmName -ControllerNumber $controllerNumber -ControllerLocation $controllerLocation -Path $path
`

	params := map[string]string{"vmName": vmName,
		"path":               path,
		"controllerNumber":   strconv.FormatInt(int64(controllerNumber), 10),
		"controllerLocation": strconv.FormatInt(int64(controllerLocation), 10)}

	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) UnmountDvdDrive(vmName string, controllerNumber uint, controllerLocation uint) error {
	var script = `
$vmName = $using:vmName
$controllerNumber = $using:controllerNumber
$controllerLocation = $using:controllerLocation

$vmDvdDrive = Get-VMDvdDrive -VMName $vmName -ControllerNumber $controllerNumber -ControllerLocation $controllerLocation
if (!$vmDvdDrive) {throw 'unable to find dvd drive'}
Set-VMDvdDrive -VMName $vmName -ControllerNumber $controllerNumber -ControllerLocation $controllerLocation -Path $null
`
	params := map[string]string{"vmName": vmName,
		"controllerNumber":   strconv.FormatInt(int64(controllerNumber), 10),
		"controllerLocation": strconv.FormatInt(int64(controllerLocation), 10)}

	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) SetBootDvdDrive(vmName string, controllerNumber uint, controllerLocation uint, generation uint) error {

	if generation < 2 {
		script := `
$vmName = $using:vmName
Set-VMBios -VMName $vmName -StartupOrder @("CD", "IDE","LegacyNetworkAdapter","Floppy")
`
		params := map[string]string{"vmName": vmName}

		_, err := hvc.Ps.OutputWinRm(script, params)
		return err
	} else {
		script := `
[string]$vmName = $using:vmName
[int]$controllerNumber = $using:controllerNumber
[int]$controllerLocation = $using:controllerLocation
$vmDvdDrive = Get-VMDvdDrive -VMName $vmName -ControllerNumber $controllerNumber -ControllerLocation $controllerLocation
if (!$vmDvdDrive) {throw 'unable to find dvd drive'}
Set-VMFirmware -VMName $vmName -FirstBootDevice $vmDvdDrive -ErrorAction SilentlyContinue
`

		params := map[string]string{"vmName": vmName,
			"controllerNumber":   strconv.FormatInt(int64(controllerNumber), 10),
			"controllerLocation": strconv.FormatInt(int64(controllerLocation), 10),
			"generation":         strconv.FormatInt(int64(generation), 10)}
		_, err := hvc.Ps.OutputWinRm(script, params)
		return err
	}
}

func (hvc *HypervRemote) DeleteDvdDrive(vmName string, controllerNumber uint, controllerLocation uint) error {
	var script = `
[string]$vmName = $using:vmName
[int]$controllerNumber = $using:controllerNumber
[int]$controllerLocation = $using:controllerLocation
$vmDvdDrive = Get-VMDvdDrive -VMName $vmName -ControllerNumber $controllerNumber -ControllerLocation $controllerLocation
if (!$vmDvdDrive) {throw 'unable to find dvd drive'}
Remove-VMDvdDrive -VMName $vmName -ControllerNumber $controllerNumber -ControllerLocation $controllerLocation
`

	params := map[string]string{"vmName": vmName,
		"controllerNumber":   strconv.FormatInt(int64(controllerNumber), 10),
		"controllerLocation": strconv.FormatInt(int64(controllerLocation), 10)}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) GetVirtualMachineId(params map[string]string) (string, error) {
	var script = `
[string]$vmName = $using:vmName

$VM = Get-VM -Name $vmName -ErrorAction SilentlyContinue | Select-Object -first 1

if ($VM) {
	$VM.Id.Guid
}
`

	Output, err := hvc.Ps.OutputWinRm(script, params)
	return Output, err
}

func (hvc *HypervRemote) GetVirtualSwitchId(params map[string]string) (string, error) {
	var script = `
[string]$Name = $using:Name
[string]$Id = $using:ID

if ($vmName) {
	$SW = Get-VMSwitch -Name $name | Select-Object -first 1
}
else {
	$SW = Get-VMSwitch -Id $id | Select-Object -first 1
}

if ($SW) {
	$SW.Id
}
`

	Output, err := hvc.Ps.OutputWinRm(script, params)
	return Output, err
}

func (hvc *HypervRemote) DeleteAllDvdDrives(vmName string) error {
	var script = `
[string]$vmName = $using:vmName
Get-VMDvdDrive -VMName $vmName | Remove-VMDvdDrive
`

	params := map[string]string{"vmName": vmName}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) MountFloppyDrive(vmName string, path string) error {
	var script = `
[string]$vmName = $using:vmName
[string]$path = $using:path
Set-VMFloppyDiskDrive -VMName $vmName -Path $path
`

	params := map[string]string{"vmName": vmName, "path": path}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) UnmountFloppyDrive(vmName string) error {

	var script = `
	[string]$vmName = $using:vmName
	Set-VMFloppyDiskDrive -VMName $vmName -Path $null
`

	params := map[string]string{"vmName": vmName}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) NewVhd(vmID, vhdName string, diskSize int64) (string, error) {

	var script = `
		[string]$vhdName = $using:vhdName
		[long]$newVHDSizeBytes = $using:diskSize
		[string]$vmID = $using:vmID

		$VM = Get-VM -Id $vmID -ErrorAction SilentlyContinue | select -first 1
		if(!$VM){Write-Error "Creating VHD for VM ID: $vmID, cannot find VM; return"}

		$vhdx = $vhdName + '.vhdx'
		$vhdPath = Join-Path -Path $VM.ConfigurationLocation -ChildPath $vhdx

		$VHD = New-VHD -Path $vhdPath -SizeBytes $newVHDSizeBytes
		Add-VMHardDiskDrive -VM $VM -Path $VHD.Path
		`
	params := map[string]string{
		"vmID":     vmID,
		"vhdName":  vhdName,
		"diskSize": strconv.FormatInt(diskSize, 10),
	}
	return hvc.Ps.OutputWinRm(script, params)
}

func (hvc *HypervRemote) NewDiskFromImagePath(vmID, vhdName, imagePath string) (string, error) {

	var script = `
	[string]$vhdName = $using:vhdName
	[string]$imagePath = $using:imagePath
	[string]$vmID = $using:vmID

	$VM = Get-VM -Id $vmID -ErrorAction SilentlyContinue | select -first 1
	if(!$VM){Write-Error "Creating VHD for VM ID: $vmID, cannot find VM; return"}

	$vhdx = $vhdName + '.vhdx'
	$vhdPath = Join-Path -Path $VM.ConfigurationLocation -ChildPath $vhdx

	if(Test-Path $imagePath){Write-Host "Cannot find VHD Image: $imagePath"}
	Copy-Item $imagePath $vhdPath

	Add-VMHardDiskDrive -VM $VM -Path $vhdPath
	`
	params := map[string]string{
		"vmID":      vmID,
		"vhdName":   vhdName,
		"imagePath": imagePath,
	}
	return hvc.Ps.OutputWinRm(script, params)
}

func (hvc *HypervRemote) NewDiskFromImageURL(vmID, vhdName, imageURL string) (string, error) {

	var script = `
			[string]$vmID = $using:vmID
			[string]$imageURL = $using:imageURL
			[string]$vhdName = $using:vhdName
			$VM = Get-VM -Id $vmID | select -first 1
			if(!$VM){Write-Error "Creating VHD for VM ID: $vmID, cannot find VM; return"}

			$vhdx = $vhdName + ".vhdx"
			$vhdPath = Join-Path -Path $VM.ConfigurationLocation -ChildPath $vhdx

			(New-Object System.Net.WebClient).DownloadFile($imageURL, $vhdPath)
			Add-VMHardDiskDrive -VM $VM -Path $vhdPath
			`

	params := map[string]string{
		"vmID":     vmID,
		"imageURL": imageURL,
		"vhdName":  vhdName,
	}

	return hvc.Ps.OutputWinRm(script, params)
}

func (hvc *HypervRemote) NewDifferencingDisk(vmID, vhdName, diffParentPath string) (string, error) {

	var script = `
	[string]$vhdName = $using:vhdName
	[string]$diffParentPath = $using:diffParentPath
	[string]$vmID = $using:vmID

	if(Test-Path $diffParentPath){Write-Host "Cannot find Differencing VHD Image: $diffParentPath"}

	$VM = Get-VM -Id $vmID -ErrorAction SilentlyContinue | select -first 1
	if(!$VM){Write-Error "Creating VHD for VM ID: $vmID, cannot find VM; return"}

	$vhdx = $vhdName + '.vhdx'

	$vhdPath = Join-Path -Path $VM.ConfigurationLocation -ChildPath $vhdx

	$VHD = New-VHD -Path $vhdpath -ParentPath $diffParentPath -Differencing
	Add-VMHardDiskDrive -VM $VM -Path $VHD.Path
	`
	params := map[string]string{
		"vmID":           vmID,
		"vhdName":        vhdName,
		"diffParentPath": diffParentPath,
	}
	return hvc.Ps.OutputWinRm(script, params)

	return hvc.Ps.OutputWinRm(script, params)
}

func (hvc *HypervRemote) CreateVirtualMachine(vmName, path string, ramMB int64, switchName string, generation int) (string, error) {

	if generation == 2 {
		var script = `
[string]$vmName = $using:vmName
[string]$path = $using:path
[long]$memoryStartupBytes = $using:ram
[string]$switchName = $using:switchName
[int]$generation = $using:generation
$VM = New-VM -Name $vmName -Path $path -MemoryStartupBytes $memoryStartupBytes -Generation $generation -SwitchName $switchName
$VM.Id.Guid
`

		params := map[string]string{"vmName": vmName,
			"path":       path,
			"ram":        strconv.FormatInt(ramMB*1024*1024, 10),
			"switchName": switchName,
			"generation": strconv.FormatInt(int64(generation), 10)}

		return hvc.Ps.OutputWinRm(script, params)

	} else {
		var script = `
[string]$vmName = $using:vmName
[string]$path = $using:path
[long]$memoryStartupBytes = $using:ram
[string]$switchName = $using:switchName
$VM = New-VM -Name $vmName -Path $path -MemoryStartupBytes $memoryStartupBytes -SwitchName $switchName -BootDevice IDE
$VM.Id.Guid
`
		params := map[string]string{"vmName": vmName,
			"path":       path,
			"ram":        strconv.FormatInt(ramMB*1024*1024, 10),
			"switchName": switchName}

		return hvc.Ps.OutputWinRm(script, params)
	}
}

func (hvc *HypervRemote) SetVirtualMachineCpuCount(vmId string, cpu int) error {

	var script = `
	[string]$vmId = $using:vmId
	[int]$cpu = $using:cpu

$VM = Get-Vm -Id $vmId
Set-VMProcessor -VM $VM -Count $cpu
`
	params := map[string]string{"vmId": vmId, "cpu": strconv.FormatInt(int64(cpu), 10)}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) SetVirtualMachineVirtualizationExtensions(vmName string, enableVirtualizationExtensions bool) error {

	var script = `
	[string]$vmName = $using:vmName
	[string]$exposeVirtualizationExtensionsString = $using:exposeVirtualizationExtensionsString
$exposeVirtualizationExtensions = [System.Boolean]::Parse($exposeVirtualizationExtensionsString)
Set-VMProcessor -VMName $vmName -ExposeVirtualizationExtensions $exposeVirtualizationExtensions
`
	exposeVirtualizationExtensionsString := "False"
	if enableVirtualizationExtensions {
		exposeVirtualizationExtensionsString = "True"
	}

	params := map[string]string{"vmName": vmName, "exposeVirtualizationExtensionsString": exposeVirtualizationExtensionsString}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) SetVirtualMachineDynamicMemory(vmName string, enableDynamicMemory bool) error {

	var script = `
	[string]$vmName = $using:vmName
	[string]$enableDynamicMemoryString = $using:enableDynamicMemoryString
$enableDynamicMemory = [System.Boolean]::Parse($enableDynamicMemoryString)
Set-VMMemory -VMName $vmName -DynamicMemoryEnabled $enableDynamicMemory
`
	enableDynamicMemoryString := "False"
	if enableDynamicMemory {
		enableDynamicMemoryString = "True"
	}
	params := map[string]string{"vmName": vmName, "enableDynamicMemoryString": enableDynamicMemoryString}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) SetVirtualMachineMacSpoofing(vmName string, enableMacSpoofing bool) error {
	var script = `
	[string]$vmName = $using:vmName
	$enableMacSpoofing = $using:enableMacSpoofing
Set-VMNetworkAdapter -VMName $vmName -MacAddressSpoofing $enableMacSpoofing
`

	enableMacSpoofingString := "Off"
	if enableMacSpoofing {
		enableMacSpoofingString = "On"
	}

	params := map[string]string{"vmName": vmName, "enableMacSpoofingString": enableMacSpoofingString}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) SetVirtualMachineSecureBoot(vmName string, enableSecureBoot bool) error {
	var script = `
	[string]$vmName = $using:vmName
	$enableSecureBoot = $using:enableSecureBoot
Set-VMFirmware -VMName $vmName -EnableSecureBoot $enableSecureBoot
`

	enableSecureBootString := "Off"
	if enableSecureBoot {
		enableSecureBootString = "On"
	}
	params := map[string]string{"vmName": vmName, "enableSecureBootString": enableSecureBootString}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) DisableNetworkBoot(vmID string) error {
	var script = `
	[string]$vmID = $using:vmID

	$VM = Get-VM -Id $vmID -ErrorAction SilentlyContinue | select -first 1
	if(!$VM){Write-Error "Creating VHD for VM ID: $vmID, cannot find VM; return"}
	$old_boot_order = Get-VMFirmware -VMName $VM.Name | Select-Object -ExpandProperty BootOrder
	$new_boot_order = $old_boot_order | Where-Object { $_.BootType -ne "Network" }
	Set-VMFirmware -VMName $VM.Name -BootOrder $new_boot_order
`

	params := map[string]string{"vmID": vmID}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) DeleteVirtualMachine(vmId string) error {

	var script = `
[string]$vmId = $using:vmId

$vm = Get-VM -Id $vmId

if (($vm.State -ne [Microsoft.HyperV.PowerShell.VMState]::Off) -and ($vm.State -ne [Microsoft.HyperV.PowerShell.VMState]::OffCritical)) {
    Stop-VM -VM $vm -TurnOff -Force -Confirm:$false
}

Remove-VM -VM $vm -Force -Confirm:$false
Start-Sleep 2
Remove-Item $VM.ConfigurationLocation -recurse -force
`
	params := map[string]string{"vmId": vmId}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) ExportVirtualMachine(vmName string, path string) error {

	var script = `
	[string]$vmName = $using:vmName
	[string]$path = $using:path
Export-VM -Name $vmName -Path $path

if (Test-Path -Path ([IO.Path]::Combine($path, $vmName, 'Virtual Machines', '*.VMCX')))
{
  $vm = Get-VM -Name $vmName
  $vm_adapter = Get-VMNetworkAdapter -VM $vm | Select -First 1

  $config = [xml]@"
<?xml version="1.0" ?>
<configuration>
  <properties>
    <subtype type="integer">$($vm.Generation - 1)</subtype>
    <name type="string">$($vm.Name)</name>
  </properties>
  <settings>
    <processors>
      <count type="integer">$($vm.ProcessorCount)</count>
    </processors>
    <memory>
      <bank>
        <dynamic_memory_enabled type="bool">$($vm.DynamicMemoryEnabled)</dynamic_memory_enabled>
        <limit type="integer">$($vm.MemoryMaximum / 1MB)</limit>
        <reservation type="integer">$($vm.MemoryMinimum / 1MB)</reservation>
        <size type="integer">$($vm.MemoryStartup / 1MB)</size>
      </bank>
    </memory>
  </settings>
  <AltSwitchName type="string">$($vm_adapter.SwitchName)</AltSwitchName>
  <boot>
    <device0 type="string">Optical</device0>
  </boot>
  <secure_boot_enabled type="bool">False</secure_boot_enabled>
  <notes type="string">$($vm.Notes)</notes>
  <vm-controllers/>
</configuration>
"@

  if ($vm.Generation -eq 1)
  {
    $vm_controllers  = Get-VMIdeController -VM $vm
    $controller_type = $config.SelectSingleNode('/configuration/vm-controllers')
    # IDE controllers are not stored in a special XML container
  }
  else
  {
    $vm_controllers  = Get-VMScsiController -VM $vm
    $controller_type = $config.CreateElement('scsi')
    $controller_type.SetAttribute('ChannelInstanceGuid', 'x')
    # SCSI controllers are stored in the scsi XML container
    if ((Get-VMFirmware -VM $vm).SecureBoot -eq [Microsoft.HyperV.PowerShell.OnOffState]::On)
    {
      $config.configuration.secure_boot_enabled.'#text' = 'True'
    }
    else
    {
      $config.configuration.secure_boot_enabled.'#text' = 'False'
    }
  }

  $vm_controllers | ForEach {
    $controller = $config.CreateElement('controller' + $_.ControllerNumber)
    $_.Drives | ForEach {
      $drive = $config.CreateElement('drive' + ($_.DiskNumber + 0))
      $drive_path = $config.CreateElement('pathname')
      $drive_path.SetAttribute('type', 'string')
      $drive_path.AppendChild($config.CreateTextNode($_.Path))
      $drive_type = $config.CreateElement('type')
      $drive_type.SetAttribute('type', 'string')
      if ($_ -is [Microsoft.HyperV.PowerShell.HardDiskDrive])
      {
        $drive_type.AppendChild($config.CreateTextNode('VHD'))
      }
      elseif ($_ -is [Microsoft.HyperV.PowerShell.DvdDrive])
      {
        $drive_type.AppendChild($config.CreateTextNode('ISO'))
      }
      else
      {
        $drive_type.AppendChild($config.CreateTextNode('NONE'))
      }
      $drive.AppendChild($drive_path)
      $drive.AppendChild($drive_type)
      $controller.AppendChild($drive)
    }
    $controller_type.AppendChild($controller)
  }
  if ($controller_type.Name -ne 'vm-controllers')
  {
    $config.SelectSingleNode('/configuration/vm-controllers').AppendChild($controller_type)
  }

  $config.Save([IO.Path]::Combine($path, $vm.Name, 'Virtual Machines', 'box.xml'))
}
`

	params := map[string]string{"vmName": vmName, "path": path}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) CompactDisks(expPath string, vhdDir string) error {
	var script = `
	[string]$srcPath = $using:srcPath
	[string]$vhdDirName = $using:vhdDirName
Get-ChildItem "$srcPath/$vhdDirName" -Filter *.vhd* | %{
    Optimize-VHD -Path $_.FullName -Mode Full
}
`
	params := map[string]string{"srcPath": expPath, "vhdDirName": vhdDir}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) CopyExportedVirtualMachine(expPath string, outputPath string, vhdDir string, vmDir string) error {

	var script = `
	[string]$srcPath = $using:srcPath
	[string]$dstPath = $using:dstPath
	[string]$vhdDirName = $using:vhdDirName
	[string]$vmDir = $using:vmDir
Move-Item -Path $srcPath/*.* -Destination $dstPath
Move-Item -Path $srcPath/$vhdDirName -Destination $dstPath
Move-Item -Path $srcPath/$vmDir -Destination $dstPath
`
	params := map[string]string{"srcPath": expPath, "dstPath": outputPath, "vhdDirName": vhdDir, "vmDir": vmDir}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) CreateVirtualSwitch(switchName string, switchType string) (string, error) {

	var script = `
[string]$switchName = $using:switchName
[string]$switchType = $using:switchType
$switches = Get-VMSwitch -Name $switchName -ErrorAction SilentlyContinue
if ($switches.Count -eq 0) {
  $SW = New-VMSwitch -Name $switchName -SwitchType $switchType
  return $SW.Id.guid
}
`

	params := map[string]string{"switchName": switchName, "switchType": switchType}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)
	return cmdOut, err
}

func (hvc *HypervRemote) AddVMNetworkAdapter(vmId, name, switchName, vlanId string) error {

	var script = `
	[string]$name = $using:name
	[string]$switchName = $using:switchName
	[string]$vlanId = $using:vlanId
	[string]$vmId = $using:vmId
	$VM = Get-VM -Id $vmID

	if(!$VM){Write-Error "Could not get VM: $VM"}
	$VM | Add-VMNetworkAdapter -Name "$name" -SwitchName "$switchName"

	# Set the boot order to disable pxe
	$OldBootOrder = Get-VMFirmware -vm $vm | Select-Object -ExpandProperty BootOrder
	$NewBootOorder = $OldBootOrder  | Where-Object { $_.BootType -ne "Network" }
	Set-VMFirmware -vm $VM -BootOrder $NewBootOorder

	if(($vlanId -ne $null) -and ($vlanId -ne "")){
		Set-VMNetworkAdapterVlan -VMNetworkAdapterName "$name" -Access -VlanId $vlanId -VMName $VM.Name
	}
	`

	params := map[string]string{"vmId": vmId, "name": name, "switchName": switchName, "vlanId": vlanId}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) DeleteVirtualSwitch(switchId string) error {

	var script = `
	[string]$switchId = $using:switchId
	# Terraform deletes resources concurrently, so lets try five times over ~25 seconds to make sure all VM's are deleted
	for ($i = 0; ($i -lt 5) -and (Get-VMSwitch -Id $switchId -ErrorAction SilentlyContinue); $i++) {
		Get-VMSwitch -Id $switchId -ErrorAction SilentlyContinue | Remove-VMSwitch -Force -Confirm:$false -ErrorAction SilentlyContinue
		Start-Sleep 5
	}

	if(Get-VMSwitch -Id $switchId -ErrorAction SilentlyContinue){
		Write-Error "Unable to delete switch after five attempts, ensure that no virtual machines are connected to it"
	}
`

	params := map[string]string{"switchId": switchId}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) StartVirtualMachine(vmName string) error {

	var script = `
	[string]$vmName = $using:vmName
$vm = Get-VM -Name $vmName -ErrorAction SilentlyContinue
if ($vm.State -eq [Microsoft.HyperV.PowerShell.VMState]::Off) {
  Start-VM -Name $vmName -Confirm:$false
}
`

	params := map[string]string{"vmName": vmName}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) RestartVirtualMachine(vmName string) error {

	var script = `
	[string]$vmName = $using:vmName
Restart-VM $vmName -Force -Confirm:$false
`

	params := map[string]string{"vmName": vmName}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) StopVirtualMachine(vmName string) error {

	var script = `
	[string]$vmName = $using:vmName
$vm = Get-VM -Name $vmName
if ($vm.State -eq [Microsoft.HyperV.PowerShell.VMState]::Running) {
    Stop-VM -VM $vm -Force -Confirm:$false
}
`

	params := map[string]string{"vmName": vmName}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) EnableVirtualMachineIntegrationService(vmName string, integrationServiceName string) error {

	integrationServiceId := ""
	switch integrationServiceName {
	case "Time Synchronization":
		integrationServiceId = "2497F4DE-E9FA-4204-80E4-4B75C46419C0"
	case "Heartbeat":
		integrationServiceId = "84EAAE65-2F2E-45F5-9BB5-0E857DC8EB47"
	case "Key-Value Pair Exchange":
		integrationServiceId = "2A34B1C2-FD73-4043-8A5B-DD2159BC743F"
	case "Shutdown":
		integrationServiceId = "9F8233AC-BE49-4C79-8EE3-E7E1985B2077"
	case "VSS":
		integrationServiceId = "5CED1297-4598-4915-A5FC-AD21BB4D02A4"
	case "Guest Service Interface":
		integrationServiceId = "6C09BB55-D683-4DA0-8931-C9BF705F6480"
	default:
		panic("unrecognized Integration Service Name")
	}

	var script = `
	[string]$vmName = $using:vmName
	[string]$integrationServiceId = $using:integrationServiceId
Get-VMIntegrationService -VmName $vmName | ?{$_.Id -match $integrationServiceId} | Enable-VMIntegrationService
`

	params := map[string]string{"vmName": vmName, "integrationServiceId": integrationServiceId}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) SetNetworkAdapterVlanId(switchName string, vlanId string) error {

	var script = `
	[string]$networkAdapterName = $using:networkAdapterName
	[string]$vlanId = $using:vlanId
Set-VMNetworkAdapterVlan -ManagementOS -VMNetworkAdapterName $networkAdapterName -Access -VlanId $vlanId
`

	params := map[string]string{"networkAdapterName": switchName, "vlanId": vlanId}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) SetNetworkAdapterStaticMacAddress(vmName, adapterName, mac string) error {

	var script = `
		[string]$vmName = $using:vmName
		[string]$vmName = $using:vmName
		[string]$mac = $using:mac
		Set-VMNetworkAdapter -VmName $vmName -VMNetworkAdapterName $adapterName -StaticMacAddress $mac
	`

	params := map[string]string{"vmName": vmName, "adapterName": adapterName, "mac": mac}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) SetVirtualMachineVlanId(vmID string, vlanId string) error {

	var script = `
[string]$vmID = $using:vmID
[string]$vlanId = $using:vlanId
$VM = Get-VM -Id $vmID
Set-VMNetworkAdapterVlan -VMName $VM.Name -Access -VlanId $vlanId
`
	params := map[string]string{"vmID": vmID, "vlanId": vlanId}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) GetExternalOnlineVirtualSwitch() (string, error) {

	var script = `
	$adapters = Get-NetAdapter -Physical -ErrorAction SilentlyContinue | Where-Object { $_.Status -eq 'Up' } | Sort-Object -Descending -Property Speed
	foreach ($adapter in $adapters) {
	  $switch = Get-VMSwitch -SwitchType External | Where-Object { $_.NetAdapterInterfaceDescription -eq $adapter.InterfaceDescription }
	  if ($switch -ne $null) {
		$switch.Name
		break
	  }
	}
	`

	cmdOut, err := hvc.Ps.OutputWinRm(script, nil)
	if err != nil {
		return "", err
	}

	var switchName = strings.TrimSpace(cmdOut)
	return switchName, nil
}

func (hvc *HypervRemote) CreateExternalVirtualSwitch(vmName string, switchName string) error {

	var script = `
[string]$vmName = $using:vmName
[string]$switchName = $using:switchName
$switch = $null
$names = @('ethernet','wi-fi','lan')
$adapters = foreach ($name in $names) {
  Get-NetAdapter -Physical -Name $name -ErrorAction SilentlyContinue | where status -eq 'up'
}

foreach ($adapter in $adapters) {
  $switch = Get-VMSwitch -SwitchType External | where { $_.NetAdapterInterfaceDescription -eq $adapter.InterfaceDescription }

  if ($switch -eq $null) {
    $switch = New-VMSwitch -Name $switchName -NetAdapterName $adapter.Name -AllowManagementOS $true -Notes 'Parent OS, VMs, WiFi'
  }

  if ($switch -ne $null) {
    break
  }
}

if($switch -ne $null) {
  Get-VMNetworkAdapter -VMName $vmName | Connect-VMNetworkAdapter -VMSwitch $switch
} else {
  Write-Error 'No internet adapters found'
}
`
	params := map[string]string{"vmName": vmName, "switchName": switchName}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) GetVirtualMachineSwitchName(vmName string) (string, error) {

	var script = `
	[string]$vmName = $using:vmName
(Get-VMNetworkAdapter -VMName $vmName).SwitchName
`

	params := map[string]string{"vmName": vmName}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(cmdOut), nil
}

func (hvc *HypervRemote) ConnectVirtualMachineNetworkAdapterToSwitch(vmName string, switchName string) error {

	var script = `
	[string]$vmName = $using:vmName
	[string]$switchName = $using:switchName
Get-VMNetworkAdapter -VMName $vmName | Connect-VMNetworkAdapter -SwitchName $switchName
`

	params := map[string]string{"vmName": vmName, "switchName": switchName}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) UntagVirtualMachineNetworkAdapterVlan(vmName string, switchName string) error {

	var script = `
	[string]$vmName = $using:vmName
	[string]$switchName = $using:switchName
Set-VMNetworkAdapterVlan -VMName $vmName -Untagged
Set-VMNetworkAdapterVlan -ManagementOS -VMNetworkAdapterName $switchName -Untagged
`

	params := map[string]string{"vmName": vmName, "switchName": switchName}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) IsRunning(vmName string) (bool, error) {

	var script = `
	[string]$vmName = $using:vmName
$vm = Get-VM -Name $vmName -ErrorAction SilentlyContinue
$vm.State -eq [Microsoft.HyperV.PowerShell.VMState]::Running
`

	params := map[string]string{"vmName": vmName}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)

	if err != nil {
		return false, err
	}

	var isRunning = strings.TrimSpace(cmdOut) == "True"
	return isRunning, err
}

func (hvc *HypervRemote) IsOff(vmName string) (bool, error) {

	var script = `
	[string]$vmName = $using:vmName
$vm = Get-VM -Name $vmName -ErrorAction SilentlyContinue
$vm.State -eq [Microsoft.HyperV.PowerShell.VMState]::Off
`

	params := map[string]string{"vmName": vmName}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)

	if err != nil {
		return false, err
	}

	var isRunning = strings.TrimSpace(cmdOut) == "True"
	return isRunning, err
}

func (hvc *HypervRemote) Uptime(vmName string) (uint64, error) {

	var script = `
	[string]$vmName = $using:vmName
$vm = Get-VM -Name $vmName -ErrorAction SilentlyContinue
$vm.Uptime.TotalSeconds
`
	params := map[string]string{"vmName": vmName}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)
	if err != nil {
		return 0, err
	}

	uptime, err := strconv.ParseUint(strings.TrimSpace(cmdOut), 10, 64)

	return uptime, err
}

func (hvc *HypervRemote) Mac(vmName string) (string, error) {
	var script = `
[string]$vmName = $using:vmName
$adapterIndex = $using:adapterIndex
try {
  $adapter = Get-VMNetworkAdapter -VMName $vmName -ErrorAction SilentlyContinue
  $mac = $adapter[$adapterIndex].MacAddress
  if($mac -eq $null) {
    return ""
  }
} catch {
  return ""
}
$mac
`

	params := map[string]string{"vmName": vmName, "adapterIndex": "0"}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)

	return cmdOut, err
}

func (hvc *HypervRemote) IpAddress(mac string) (string, error) {
	var script = `
	[string]$mac = $using:mac
	[int]$addressIndex = $using:adapterIndex
try {
  $ip = Get-Vm | %{$_.NetworkAdapters} | ?{$_.MacAddress -eq $mac} | %{$_.IpAddresses[$addressIndex]}

  if($ip -eq $null) {
    return ""
  }
} catch {
  return ""
}
$ip
`

	params := map[string]string{"mac": mac, "adapterIndex": "0"}
	cmdOut, err := hvc.Ps.OutputWinRm(script, params)

	return cmdOut, err
}

func (hvc *HypervRemote) TurnOff(vmName string) error {

	var script = `
	[string]$vmName = $using:vmName
$vm = Get-VM -Name $vmName -ErrorAction SilentlyContinue
if ($vm.State -eq [Microsoft.HyperV.PowerShell.VMState]::Running) {
  Stop-VM -Name $vmName -TurnOff -Force -Confirm:$false
}
`
	params := map[string]string{"vmName": vmName}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) ShutDown(vmName string) error {

	var script = `
	[string]$vmName = $using:vmName
$vm = Get-VM -Name $vmName -ErrorAction SilentlyContinue
if ($vm.State -eq [Microsoft.HyperV.PowerShell.VMState]::Running) {
  Stop-VM -Name $vmName -Force -Confirm:$false
}
`

	params := map[string]string{"vmName": vmName}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}

func (hvc *HypervRemote) TypeScanCodes(vmName string, scanCodes string) error {
	if len(scanCodes) == 0 {
		return nil
	}

	var script = `
	[string]$vmName = $using:vmName
	[string]$scanCodes = $using:scanCodes
	#Requires -Version 3

	function Get-VMConsole
	{
	    [CmdletBinding()]
	    param (
	        [Parameter(Mandatory)]
	        [string] $VMName
	    )

	    $ErrorActionPreference = "Stop"

	    $vm = Get-CimInstance -Namespace "root\virtualization\v2" -ClassName Msvm_ComputerSystem -ErrorAction Ignore -Verbose:$false | where ElementName -eq $VMName | select -first 1
	    if ($vm -eq $null){
	        Write-Error ("VirtualMachine({0}) is not found!" -f $VMName)
	    }

	    $vmKeyboard = $vm | Get-CimAssociatedInstance -ResultClassName "Msvm_Keyboard" -ErrorAction Ignore -Verbose:$false

		if ($vmKeyboard -eq $null) {
			$vmKeyboard = Get-CimInstance -Namespace "root\virtualization\v2" -ClassName Msvm_Keyboard -ErrorAction Ignore -Verbose:$false | where SystemName -eq $vm.Name | select -first 1
		}

		if ($vmKeyboard -eq $null) {
			$vmKeyboard = Get-CimInstance -Namespace "root\virtualization" -ClassName Msvm_Keyboard -ErrorAction Ignore -Verbose:$false | where SystemName -eq $vm.Name | select -first 1
		}

	    if ($vmKeyboard -eq $null){
	        Write-Error ("VirtualMachine({0}) keyboard class is not found!" -f $VMName)
	    }

	    #TODO: It may be better using New-Module -AsCustomObject to return console object?

	    #Console object to return
	    $console = [pscustomobject] @{
	        Msvm_ComputerSystem = $vm
	        Msvm_Keyboard = $vmKeyboard
	    }

	    #Need to import assembly to use System.Windows.Input.Key
	    Add-Type -AssemblyName WindowsBase

	    #region Add Console Members
	    $console | Add-Member -MemberType ScriptMethod -Name TypeText -Value {
	        [OutputType([bool])]
	        param (
	            [ValidateNotNullOrEmpty()]
	            [Parameter(Mandatory)]
	            [string] $AsciiText
	        )
	        $result = $this.Msvm_Keyboard | Invoke-CimMethod -MethodName "TypeText" -Arguments @{ asciiText = $AsciiText }
	        return (0 -eq $result.ReturnValue)
	    }

	    #Define method:TypeCtrlAltDel
	    $console | Add-Member -MemberType ScriptMethod -Name TypeCtrlAltDel -Value {
	        $result = $this.Msvm_Keyboard | Invoke-CimMethod -MethodName "TypeCtrlAltDel"
	        return (0 -eq $result.ReturnValue)
	    }

	    #Define method:TypeKey
	    $console | Add-Member -MemberType ScriptMethod -Name TypeKey -Value {
	        [OutputType([bool])]
	        param (
	            [Parameter(Mandatory)]
	            [Windows.Input.Key] $Key,
	            [Windows.Input.ModifierKeys] $ModifierKey = [Windows.Input.ModifierKeys]::None
	        )

	        $keyCode = [Windows.Input.KeyInterop]::VirtualKeyFromKey($Key)

	        switch ($ModifierKey)
	        {
	            ([Windows.Input.ModifierKeys]::Control){ $modifierKeyCode = [Windows.Input.KeyInterop]::VirtualKeyFromKey([Windows.Input.Key]::LeftCtrl)}
	            ([Windows.Input.ModifierKeys]::Alt){ $modifierKeyCode = [Windows.Input.KeyInterop]::VirtualKeyFromKey([Windows.Input.Key]::LeftAlt)}
	            ([Windows.Input.ModifierKeys]::Shift){ $modifierKeyCode = [Windows.Input.KeyInterop]::VirtualKeyFromKey([Windows.Input.Key]::LeftShift)}
	            ([Windows.Input.ModifierKeys]::Windows){ $modifierKeyCode = [Windows.Input.KeyInterop]::VirtualKeyFromKey([Windows.Input.Key]::LWin)}
	        }

	        if ($ModifierKey -eq [Windows.Input.ModifierKeys]::None)
	        {
	            $result = $this.Msvm_Keyboard | Invoke-CimMethod -MethodName "TypeKey" -Arguments @{ keyCode = $keyCode }
	        }
	        else
	        {
	            $this.Msvm_Keyboard | Invoke-CimMethod -MethodName "PressKey" -Arguments @{ keyCode = $modifierKeyCode }
	            $result = $this.Msvm_Keyboard | Invoke-CimMethod -MethodName "TypeKey" -Arguments @{ keyCode = $keyCode }
	            $this.Msvm_Keyboard | Invoke-CimMethod -MethodName "ReleaseKey" -Arguments @{ keyCode = $modifierKeyCode }
	        }
	        $result = return (0 -eq $result.ReturnValue)
	    }

	    #Define method:Scancodes
	    $console | Add-Member -MemberType ScriptMethod -Name TypeScancodes -Value {
	        [OutputType([bool])]
	        param (
	            [Parameter(Mandatory)]
	            [byte[]] $ScanCodes
	        )
	        $result = $this.Msvm_Keyboard | Invoke-CimMethod -MethodName "TypeScancodes" -Arguments @{ ScanCodes = $ScanCodes }
	        return (0 -eq $result.ReturnValue)
	    }

	    #Define method:ExecCommand
	    $console | Add-Member -MemberType ScriptMethod -Name ExecCommand -Value {
	        param (
	            [Parameter(Mandatory)]
	            [string] $Command
	        )
	        if ([String]::IsNullOrEmpty($Command)){
	            return
	        }

	        $console.TypeText($Command) > $null
	        $console.TypeKey([Windows.Input.Key]::Enter) > $null
	        #sleep -Milliseconds 100
	    }

	    #Define method:Dispose
	    $console | Add-Member -MemberType ScriptMethod -Name Dispose -Value {
	        $this.Msvm_ComputerSystem.Dispose()
	        $this.Msvm_Keyboard.Dispose()
	    }


	    #endregion

	    return $console
	}

	$vmConsole = Get-VMConsole -VMName $vmName
	$scanCodesToSend = ''
	$scanCodes.Split(' ') | %{
		$scanCode = $_

		if ($scanCode.StartsWith('wait')){
			$timeToWait = $scanCode.Substring(4)
			if (!$timeToWait){
				$timeToWait = "1"
			}

			if ($scanCodesToSend){
				$scanCodesToSendByteArray = [byte[]]@($scanCodesToSend.Split(' ') | %{"0x$_"})

                $scanCodesToSendByteArray | %{
				    $vmConsole.TypeScancodes($_)
                }
			}

			write-host "Special code <wait> found, will sleep $timeToWait second(s) at this point."
			Start-Sleep -s $timeToWait

			$scanCodesToSend = ''
		} else {
			if ($scanCodesToSend){
				write-host "Sending special code '$scanCodesToSend' '$scanCode'"
				$scanCodesToSend = "$scanCodesToSend $scanCode"
			} else {
				write-host "Sending char '$scanCode'"
				$scanCodesToSend = "$scanCode"
			}
		}
	}
	if ($scanCodesToSend){
		$scanCodesToSendByteArray = [byte[]]@($scanCodesToSend.Split(' ') | %{"0x$_"})

        $scanCodesToSendByteArray | %{
			$vmConsole.TypeScancodes($_)
        }
	}
`

	params := map[string]string{"vmName": vmName, "scanCodes": scanCodes}
	_, err := hvc.Ps.OutputWinRm(script, params)
	return err
}
