package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"libvirt.org/go/libvirt"
)

func respondJSON(
	w http.ResponseWriter,
	status int,
	data any,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

type MachineState struct {
	State     int    `json:"state"`
	MaxRam    uint64 `json:"max_ram"`
	Ram       uint64 `json:"ram"`
	CoreCount uint   `json:"core_count"`
	CpuTime   uint64 `json:"cpu_time"`
}

func main() {
	cnn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		fmt.Println("Ошибка подключения к гипервизору: ", err)
		os.Exit(1)
	}
	defer cnn.Close()
	const domainXML = `
	<domain type='kvm'>
		<name>ubuntu-vm</name>
		<memory unit='G'>6</memory>
		<vcpu>3</vcpu>
		<os>
			<type arch='x86_64'>hvm</type>
			<boot dev='hd'/>
			<boot dev='cdrom'/>
		</os>
		<cpu mode='host-passthrough'/>
		<devices>
			<disk type='file' device='disk'>
			<driver name='qemu' type='qcow2'/>
			<source file='/home/bob/fun/ubuntu-vm/ubuntu.qcow2'/>
			<target dev='vda' bus='virtio'/>
			</disk>
			<disk type='file' device='cdrom'>
			<source file='/home/bob/fun/ubuntu-vm/ubuntu-24.04.4-live-server-amd64.iso'/>
			<target dev='sda' bus='sata'/>
			<readonly/>
			</disk>
			<interface type='user'>
				<model type='virtio'/>
			</interface>
			<graphics type='spice' port='5930'>
			<image compression='off'/>
			</graphics>
			<video>
			<model type='qxl'/>
			</video>
		</devices>
	</domain>`
	// in interface scope 👇🏾
	// <backend type='passt'/>
	// <portForward proto='tcp'>
	// 	<range start='2222' to='22'/>
	// </portForward>
	var dom *libvirt.Domain
	dom, err = cnn.LookupDomainByName("ubuntu-vm")
	if err != nil {
		dom, err = cnn.DomainDefineXML(domainXML)
		if err != nil {
			log.Fatalln("Ошибка создания виртуальной машины: ", err)
		}
	}
	defer dom.Free()
	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"}, // или конкретный "*"
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	}))
	r.Get("/start", func(resp http.ResponseWriter, req *http.Request) {
		if err := dom.Create(); err != nil {
			log.Println("Ошибка запуска виртуальной машины: ", err)
			respondJSON(resp, 500, "Ошибка запуска виртульной машины")
			return
		}
		respondJSON(resp, 200, "Виртуальная машина успешно запущенна!")
	})
	r.Get("/forcestop", func(resp http.ResponseWriter, req *http.Request) {
		if err := dom.Destroy(); err != nil {
			log.Println("Ошибка отключения виртуальной машины: ", err)
			respondJSON(resp, 500, "Ошибка отключения виртульной машины")
			return
		}
		respondJSON(resp, 200, "Виртуальная машина успешно отключена!")
	})
	r.Get("/stop", func(resp http.ResponseWriter, req *http.Request) {
		if err := dom.Shutdown(); err != nil {
			log.Println("Ошибка отключения виртуальной машины: ", err)
			respondJSON(resp, 500, "Ошибка отключения виртульной машины")
			return
		}
		respondJSON(resp, 200, "Виртуальная машина успешно отключена!")
	})
	r.Get("/reboot", func(resp http.ResponseWriter, req *http.Request) {
		if err := dom.Reboot(0); err != nil {
			log.Println("Ошибка перезагрузки виртуальной машины: ", err)
			respondJSON(resp, 500, "Ошибка перезагрузки виртульной машины")
			return
		}
		respondJSON(resp, 200, "Виртуальная машина успешно перезагружается!")
	})
	r.Get("/suspend", func(resp http.ResponseWriter, req *http.Request) {
		if err := dom.Suspend(); err != nil {
			log.Println("Ошибка приостоновления виртуальной машины: ", err)
			respondJSON(resp, 500, "Ошибка приостоновления виртульной машины")
			return
		}
		respondJSON(resp, 200, "Виртуальная машина успешно приостановлена!")
	})
	r.Get("/resume", func(resp http.ResponseWriter, req *http.Request) {
		if err := dom.Resume(); err != nil {
			log.Println("Ошибка возообновления виртуальной машины: ", err)
			respondJSON(resp, 500, "Ошибка возообновления виртульной машины")
			return
		}
		respondJSON(resp, 200, "Виртуальная машина успешно возообновлена!")
	})
	r.Get("/state", func(resp http.ResponseWriter, req *http.Request) {
		state, err := dom.GetInfo()
		if err != nil {
			log.Println("Ошибка получения состояния VM: ", err)
			respondJSON(resp, 500, "Ошибка получения состояния VM")
			return
		}
		machineState := MachineState{
			State:     int(state.State),
			MaxRam:    state.MaxMem,
			Ram:       state.Memory,
			CoreCount: state.NrVirtCpu,
			CpuTime:   state.CpuTime,
		}
		respondJSON(resp, 200, machineState)
	})
	http.ListenAndServe("0.0.0.0:8080", r)
}
