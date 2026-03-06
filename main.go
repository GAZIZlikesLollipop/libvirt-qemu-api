package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/gorilla/websocket"
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

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Hub struct {
	clients    map[*websocket.Conn]bool
	brodcast   chan []byte
	register   chan *websocket.Conn
	unRegister chan *websocket.Conn
}

func newHub() Hub {
	return Hub{
		clients:    make(map[*websocket.Conn]bool),
		brodcast:   make(chan []byte),
		register:   make(chan *websocket.Conn),
		unRegister: make(chan *websocket.Conn),
	}
}

func (h *Hub) run() {
	for {
		select {
		case conn := <-h.register:
			h.clients[conn] = true
		case conn := <-h.unRegister:
			delete(h.clients, conn)
			conn.Close()
		case data := <-h.brodcast:
			for conn := range h.clients {
				conn.WriteMessage(websocket.TextMessage, data)
			}
		}
	}
}

func listenState(h *Hub, dom *libvirt.Domain) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			fmt.Println("Ошибка создания webSocket соединения: ", err)
			w.Write([]byte("Ошибка создения webSocket соединения"))
			return
		}
		h.register <- conn
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				log.Println(err)
				h.unRegister <- conn
				break
			}
			state, err := dom.GetInfo()
			var domainStates = map[libvirt.DomainState]string{
				libvirt.DOMAIN_NOSTATE:     "Unknown",
				libvirt.DOMAIN_RUNNING:     "Started",
				libvirt.DOMAIN_BLOCKED:     "Resumed",
				libvirt.DOMAIN_PAUSED:      "Suspended",
				libvirt.DOMAIN_SHUTDOWN:    "Shutdown",
				libvirt.DOMAIN_CRASHED:     "Crashed",
				libvirt.DOMAIN_PMSUSPENDED: "Pmsuspended",
				libvirt.DOMAIN_SHUTOFF:     "Stopped",
			}
			data, err := json.Marshal(
				&MachineState{
					Event:     domainStates[state.State],
					Detail:    "",
					Memory:    state.MaxMem,
					CoreCount: state.NrVirtCpu,
					CpuTime:   state.CpuTime,
				},
			)
			if err != nil {
				log.Println("Ошибка преобрзования события в json: ", err)
				return
			}
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}
}

type MachineState struct {
	Event     string `json:"event"`
	Detail    string `json:"detail"`
	Memory    uint64 `json:"memory"`
	CoreCount uint   `json:"core_count"`
	CpuTime   uint64 `json:"cpu_time"`
}

func eventAndDetail(event *libvirt.DomainEventLifecycle) (string, string) {
	switch event.Event {

	case libvirt.DOMAIN_EVENT_DEFINED:
		details := map[libvirt.DomainEventDefinedDetailType]string{
			libvirt.DOMAIN_EVENT_DEFINED_ADDED:         "added",
			libvirt.DOMAIN_EVENT_DEFINED_UPDATED:       "updated",
			libvirt.DOMAIN_EVENT_DEFINED_RENAMED:       "renamed",
			libvirt.DOMAIN_EVENT_DEFINED_FROM_SNAPSHOT: "from snapshot",
		}
		d, ok := details[libvirt.DomainEventDefinedDetailType(event.Detail)]
		if !ok {
			d = "unknown"
		}
		return "Defined", d

	case libvirt.DOMAIN_EVENT_UNDEFINED:
		details := map[libvirt.DomainEventUndefinedDetailType]string{
			libvirt.DOMAIN_EVENT_UNDEFINED_REMOVED: "removed",
			libvirt.DOMAIN_EVENT_UNDEFINED_RENAMED: "renamed",
		}
		d, ok := details[libvirt.DomainEventUndefinedDetailType(event.Detail)]
		if !ok {
			d = "unknown"
		}
		return "Undefined", d

	case libvirt.DOMAIN_EVENT_STARTED:
		details := map[libvirt.DomainEventStartedDetailType]string{
			libvirt.DOMAIN_EVENT_STARTED_BOOTED:        "booted",
			libvirt.DOMAIN_EVENT_STARTED_MIGRATED:      "migrated from another host",
			libvirt.DOMAIN_EVENT_STARTED_RESTORED:      "restored from saved state",
			libvirt.DOMAIN_EVENT_STARTED_FROM_SNAPSHOT: "started from snapshot",
			libvirt.DOMAIN_EVENT_STARTED_WAKEUP:        "woke up from sleep",
		}
		d, ok := details[libvirt.DomainEventStartedDetailType(event.Detail)]
		if !ok {
			d = "unknown"
		}
		return "Started", d

	case libvirt.DOMAIN_EVENT_SUSPENDED:
		details := map[libvirt.DomainEventSuspendedDetailType]string{
			libvirt.DOMAIN_EVENT_SUSPENDED_PAUSED:          "paused by user",
			libvirt.DOMAIN_EVENT_SUSPENDED_MIGRATED:        "paused during migration",
			libvirt.DOMAIN_EVENT_SUSPENDED_IOERROR:         "paused due to disk error",
			libvirt.DOMAIN_EVENT_SUSPENDED_WATCHDOG:        "paused by watchdog",
			libvirt.DOMAIN_EVENT_SUSPENDED_RESTORED:        "paused after restore",
			libvirt.DOMAIN_EVENT_SUSPENDED_FROM_SNAPSHOT:   "paused from snapshot",
			libvirt.DOMAIN_EVENT_SUSPENDED_API_ERROR:       "paused due to api error",
			libvirt.DOMAIN_EVENT_SUSPENDED_POSTCOPY:        "paused during postcopy migration",
			libvirt.DOMAIN_EVENT_SUSPENDED_POSTCOPY_FAILED: "paused because postcopy failed",
		}
		d, ok := details[libvirt.DomainEventSuspendedDetailType(event.Detail)]
		if !ok {
			d = "unknown"
		}
		return "Suspended", d

	case libvirt.DOMAIN_EVENT_RESUMED:
		details := map[libvirt.DomainEventResumedDetailType]string{
			libvirt.DOMAIN_EVENT_RESUMED_UNPAUSED:      "resumed by user",
			libvirt.DOMAIN_EVENT_RESUMED_MIGRATED:      "resumed after migration",
			libvirt.DOMAIN_EVENT_RESUMED_FROM_SNAPSHOT: "resumed from snapshot",
			libvirt.DOMAIN_EVENT_RESUMED_POSTCOPY:      "resumed in postcopy migration",
		}
		d, ok := details[libvirt.DomainEventResumedDetailType(event.Detail)]
		if !ok {
			d = "unknown"
		}
		return "Resumed", d

	case libvirt.DOMAIN_EVENT_STOPPED:
		details := map[libvirt.DomainEventStoppedDetailType]string{
			libvirt.DOMAIN_EVENT_STOPPED_SHUTDOWN:      "shut down gracefully",
			libvirt.DOMAIN_EVENT_STOPPED_DESTROYED:     "force killed",
			libvirt.DOMAIN_EVENT_STOPPED_CRASHED:       "crashed",
			libvirt.DOMAIN_EVENT_STOPPED_MIGRATED:      "migrated to another host",
			libvirt.DOMAIN_EVENT_STOPPED_SAVED:         "saved to disk",
			libvirt.DOMAIN_EVENT_STOPPED_FAILED:        "stopped due to host error",
			libvirt.DOMAIN_EVENT_STOPPED_FROM_SNAPSHOT: "stopped by snapshot revert",
		}
		d, ok := details[libvirt.DomainEventStoppedDetailType(event.Detail)]
		if !ok {
			d = "unknown"
		}
		return "Stopped", d

	case libvirt.DOMAIN_EVENT_SHUTDOWN:
		details := map[libvirt.DomainEventShutdownDetailType]string{
			libvirt.DOMAIN_EVENT_SHUTDOWN_FINISHED: "finished shutting down",
			libvirt.DOMAIN_EVENT_SHUTDOWN_GUEST:    "guest initiated shutdown",
			libvirt.DOMAIN_EVENT_SHUTDOWN_HOST:     "host initiated shutdown",
		}
		d, ok := details[libvirt.DomainEventShutdownDetailType(event.Detail)]
		if !ok {
			d = "unknown"
		}
		return "Shutdown", d

	case libvirt.DOMAIN_EVENT_CRASHED:
		details := map[libvirt.DomainEventCrashedDetailType]string{
			libvirt.DOMAIN_EVENT_CRASHED_PANICKED:    "kernel panic",
			libvirt.DOMAIN_EVENT_CRASHED_CRASHLOADED: "crash kernel loaded",
		}
		d, ok := details[libvirt.DomainEventCrashedDetailType(event.Detail)]
		if !ok {
			d = "unknown"
		}
		return "Crashed", d

	case libvirt.DOMAIN_EVENT_PMSUSPENDED:
		details := map[libvirt.DomainEventPMSuspendedDetailType]string{
			libvirt.DOMAIN_EVENT_PMSUSPENDED_MEMORY: "suspended to ram",
			libvirt.DOMAIN_EVENT_PMSUSPENDED_DISK:   "suspended to disk",
		}
		d, ok := details[libvirt.DomainEventPMSuspendedDetailType(event.Detail)]
		if !ok {
			d = "unknown"
		}
		return "Pmsuspended", d
	}

	return "Unknown", "unknown"
}

func initLibvirt(h *Hub) (*libvirt.Domain, *int, *libvirt.Connect, error) {
	if err := libvirt.EventRegisterDefaultImpl(); err != nil {
		log.Println("Ошибка регистрации кастомного event loop")
		return nil, nil, nil, err
	}
	cnn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		log.Println("Ошибка подключения к гипервизору")
		return nil, nil, nil, err
	}
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
		<features>
			<acpi/>
			<apic/>
		</features>
		<pm>
			<suspend-to-mem enabled='yes'/>
			<suspend-to-disk enabled='yes'/>
		</pm>
		<cpu mode='host-passthrough'/>
		<devices>
			<disk type='file' device='disk'>
				<driver name='qemu' type='qcow2'/>
				<source file='/home/bob/Public/ubuntu-vm/ubuntu.qcow2'/>
				<target dev='vda' bus='virtio'/>
			</disk>
			<disk type='file' device='cdrom'>
				<source file='/home/bob/Public/ubuntu-vm/ubuntu-24.04.4-live-server-amd64.iso'/>
				<target dev='sda' bus='sata'/>
				<readonly/>
			</disk>
			<interface type='user'>
				<backend type='passt'/>
				<model type='virtio'/>
				<portForward proto='tcp'>
					<range start='2222' to='22'/>
				</portForward>
			</interface>
			<channel type='unix'>
				<target type='virtio' name='org.qemu.guest_agent.0'/>
			</channel>
			<graphics type='spice' port='5930'>
				<image compression='off'/>
			</graphics>
			<video>
				<model type='qxl'/>
			</video>
		</devices>
	</domain>`

	var dom *libvirt.Domain
	dom, err = cnn.LookupDomainByName("ubuntu-vm")
	if err != nil {
		dom, err = cnn.DomainDefineXML(domainXML)
		if err != nil {
			log.Println("Ошибка создания виртуальной машины")
			return nil, nil, nil, err
		}
	}

	callbackId, err := cnn.DomainEventLifecycleRegister(
		dom,
		func(c *libvirt.Connect, d *libvirt.Domain, event *libvirt.DomainEventLifecycle) {
			evnt, detail := eventAndDetail(event)
			state, err := d.GetInfo()
			if err != nil {
				log.Println("Ошибка получения инфорамции об виртуальной машине: ", err)
				return
			}
			data, err := json.Marshal(
				&MachineState{
					Event:     evnt,
					Detail:    detail,
					Memory:    state.MaxMem,
					CoreCount: state.NrVirtCpu,
					CpuTime:   state.CpuTime,
				},
			)
			if err != nil {
				log.Println("Ошибка преобрзования события в json: ", err)
				return
			}
			h.brodcast <- data
		},
	)
	if err != nil {
		log.Println("Ошибка регистрации на поток свежик данных об lifecycle of machine")
		return nil, nil, nil, err
	}
	go func() {
		for {
			if err := libvirt.EventRunDefaultImpl(); err != nil {
				log.Println("event loop ошибка:", err)
			}
		}
	}()
	return dom, &callbackId, cnn, nil
}

func main() {
	hub := newHub()
	go hub.run()

	dom, cbkId, cnn, err := initLibvirt(&hub)
	if err != nil {
		panic(err)
	}
	defer dom.Free()
	defer cnn.DomainEventDeregister(*cbkId)
	defer cnn.Close()

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
		if state, _, _ := dom.GetState(); state == 7 {
			if err := dom.PMWakeup(0); err != nil {
				log.Println("Ошибка возообновления виртуальной машины: ", err)
				respondJSON(resp, 500, "Ошибка возообновления виртульной машины")
				return
			}
		} else {
			if err := dom.Resume(); err != nil {
				log.Println("Ошибка возообновления виртуальной машины: ", err)
				respondJSON(resp, 500, "Ошибка возообновления виртульной машины")
				return
			}
		}
		respondJSON(resp, 200, "Виртуальная машина успешно возообновлена!")
	})
	r.Get("/state", listenState(&hub, dom))
	http.ListenAndServe("0.0.0.0:8080", r)
}
