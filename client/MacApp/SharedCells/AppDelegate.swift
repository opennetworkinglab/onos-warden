//
//  AppDelegate.swift
//  SharedCells
//
//  Created by Thomas Vachuska on 5/13/16.
//  Copyright © 2016 Thomas Vachuska. All rights reserved.
//

import Cocoa
fileprivate func < <T : Comparable>(lhs: T?, rhs: T?) -> Bool {
  switch (lhs, rhs) {
  case let (l?, r?):
    return l < r
  case (nil, _?):
    return true
  default:
    return false
  }
}


@NSApplicationMain
class AppDelegate: NSObject, NSApplicationDelegate, NSUserNotificationCenterDelegate {

    @IBOutlet weak var window: NSWindow!

    let wardenUrl = "http://10.254.1.19:4321/"
    let pollSeconds = 30.0
    let showSeconds = 15.0
    let warnMinutes = 5
    let defaultDurationMinutes = 120 // minutes
    
    let username = NSUserName()
    let center = NSUserNotificationCenter.default
    let statusItem = NSStatusBar.system().statusItem(withLength: -2)
    let popover = NSPopover()

    var button: NSStatusBarButton?
    var timer: Timer?
    var closeTimer: Timer?
    var notificationTimer: Timer?
    var eventMonitor: EventMonitor?
    var notification: NSUserNotification?
    
    var hadReservation = false
    var pendingAction = false
    var cellsTableController: SharedCellsViewController?

    // Start-up hook
    func applicationDidFinishLaunching(_ aNotification: Notification) {
        button = statusItem.button
        if button != nil {
            button?.image = NSImage(named: "Image")
            button?.image?.isTemplate = true
            button?.action = #selector(AppDelegate.togglePopover(_:))
        }
        
        popover.contentViewController = SharedCellsViewController(nibName: "SharedCellsViewController", bundle: nil)
        
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "View Cells", action: #selector(viewCells(_:)), keyEquivalent: "s"))
        menu.addItem(NSMenuItem.separator())
        menu.addItem(NSMenuItem(title: "Borrow Standard Cell", action: #selector(borrow31Cell), keyEquivalent: "b"))
        
        let subMenuItem = NSMenuItem(title: "Borrow Custom Cell", action: nil, keyEquivalent: "")
        menu.addItem(subMenuItem)
        
        let subMenu = NSMenu()
        subMenu.addItem(NSMenuItem(title: "Borrow 1+1 Cell", action: #selector(borrow11Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 3+1 Cell", action: #selector(borrow31Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 5+1 Cell", action: #selector(borrow51Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 7+1 Cell", action: #selector(borrow71Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem.separator())
        subMenu.addItem(NSMenuItem(title: "Borrow 1+0 Cell", action: #selector(borrow10Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 3+0 Cell", action: #selector(borrow30Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 5+0 Cell", action: #selector(borrow50Cell), keyEquivalent: ""))
        subMenu.addItem(NSMenuItem(title: "Borrow 7+0 Cell", action: #selector(borrow70Cell), keyEquivalent: ""))
        menu.setSubmenu(subMenu, for: subMenuItem)

        menu.addItem(NSMenuItem.separator())
        menu.addItem(NSMenuItem(title: "Return Cell", action: #selector(returnCell(_:)), keyEquivalent: "r"))
        menu.addItem(NSMenuItem.separator())
        menu.addItem(NSMenuItem(title: "Quit", action: #selector(NSApplication.terminate), keyEquivalent: "q"))
        statusItem.menu = menu
        
        center.delegate = self
        cellsTableController = popover.contentViewController as? SharedCellsViewController
        
        timer = Timer.scheduledTimer(timeInterval: pollSeconds, target: self,
                                                       selector: #selector(checkForExpiration),
                                                       userInfo: nil, repeats: true)
        
        eventMonitor = EventMonitor(mask: [.leftMouseDown, .rightMouseDown]) { [unowned self] event in
            if self.popover.isShown {
                self.closePopover(event)
            }
        }
        eventMonitor?.start()
        checkStatus()
    }

    // Tear-down hook
    func applicationWillTerminate(_ aNotification: Notification) {
        // Insert code here to tear down your application
        timer?.invalidate()
        center.removeAllDeliveredNotifications()
    }

    // Obtains data on cell status and displays it in a pop-up window
    func viewCells(_ sender: AnyObject?) {
        request("\(wardenUrl)/data", method: "GET", stringData: nil, callback: updatePopover, errorCallback: {
            self.showNotification("Unable to query cells", text: "Please connect to the ON.Lab VPN", action: nil, sound: false)
        })
        showPopover(self)
    }
    
    func borrow11Cell() { borrowCell("1%2B1") }
    func borrow31Cell() { borrowCell("3%2B1") }
    func borrow51Cell() { borrowCell("5%2B1") }
    func borrow71Cell() { borrowCell("7%2B1") }
    func borrow10Cell() { borrowCell("1%2B0") }
    func borrow30Cell() { borrowCell("3%2B0") }
    func borrow50Cell() { borrowCell("5%2B0") }
    func borrow70Cell() { borrowCell("7%2B0") }
    
    // Borrows cell, or extends existing reservation, for the user and for default number of minutes into the future
    func borrowCell(_ cellSpec: String) {
        self.showNotification("Allocating cell", text: "Please wait for confirmation", action: nil, sound: false)
        pendingAction = true
        request("\(wardenUrl)?duration=\(defaultDurationMinutes)&user=\(username)&spec=\(cellSpec)", method: "POST",
                stringData: userKey()! as String, callback: { response in
            self.notification = self.showNotification("Cell is allocated and ready",
                                                     text: "Reservation is valid for \(self.defaultDurationMinutes) minutes", action: nil, sound: false)
            self.scheduleNotificationDismissal()
            self.pendingAction = false
            }, errorCallback: {
                self.showNotification("Unable to borrow cell", text: "Please connect to the ON.Lab VPN", action: nil, sound: false)
            })
    }

    // Returns cell currently leased by the user
    func returnCell(_ sender: AnyObject?) {
        pendingAction = true
        self.setHaveReservation(false)
        self.showNotification("Returning cell", text: "Tearing down the environment", action: nil, sound: false)
        request("\(wardenUrl)?user=\(username)", method: "DELETE", stringData: nil, callback: { response in
            self.notification = self.showNotification("Cell returned", text: "Thank you for cleaning up!", action: nil, sound: false)
            self.scheduleNotificationDismissal()
            self.pendingAction = false
            }, errorCallback: {
                self.showNotification("Unable to return cell", text: "Please connect to the ON.Lab VPN", action: nil, sound: false)
            })
    }

    // Extends the current cell lease.
    func extendLease() {
        request("\(wardenUrl)?duration=\(defaultDurationMinutes)&user=\(username)", method: "POST",
                stringData: userKey()! as String, callback: { response in
            self.notification = self.showNotification("Cell lease extended", text: "Reservation is valid for \(self.defaultDurationMinutes) minutes", action: nil, sound: false)
            self.scheduleNotificationDismissal()
            }, errorCallback: {
                self.showNotification("Unable to extend lease", text: "Please connect to the ON.Lab VPN", action: nil, sound: false)
            })
    }
    
    // Extracts the user's public key from the ~/.ssh folder.
    func userKey() -> NSString? {
        let home = NSHomeDirectory()
        let sshKeyFilePath = home + "/.ssh/id_rsa.pub" as String
        return try? NSString(contentsOfFile: sshKeyFilePath, encoding: String.Encoding.utf8.rawValue)
    }

    func updatePopover(_ data: NSString) {
        cellsTableController?.updateCellData(data)
    }

    func showPopover(_ sender: AnyObject?) {
        if let button = statusItem.button {
            popover.show(relativeTo: button.bounds, of: button, preferredEdge: NSRectEdge.minY)
            closeTimer = Timer.scheduledTimer(timeInterval: showSeconds, target: self,
                                                                selector: #selector(closePopover(_:)),
                                                                userInfo: nil, repeats: false)
        }
        eventMonitor?.start()
    }

    func closePopover(_ sender: AnyObject?) {
        closeTimer?.invalidate()
        popover.performClose(sender)
        eventMonitor?.stop()
    }

    func togglePopover(_ sender: AnyObject?) {
        if popover.isShown {
            closePopover(sender)
        } else {
            showPopover(sender)
        }
    }

    // Schedules dismissal of a notification using the main run loop.
    func scheduleNotificationDismissal() {
        closeTimer = Timer(timeInterval: showSeconds, target: self, selector: #selector(dismissNotification), userInfo: nil, repeats: false)
        RunLoop.main.add(closeTimer!, forMode: RunLoopMode.commonModes)
    }
    
    // Dismisses a notification if there is one pending.
    func dismissNotification() {
        if notification != nil {
            center.removeDeliveredNotification(notification!)
        }
    }

    // Shows a user notification using the supplied information.
    func showNotification(_ title: String, text: String, action: String?, sound: Bool) -> NSUserNotification {
        center.removeAllDeliveredNotifications()
        let notification = NSUserNotification()
        notification.title = title
        notification.informativeText = text
        notification.hasActionButton = action != nil
        if notification.hasActionButton {
            notification.actionButtonTitle = action!
        }
        if sound {
            notification.soundName = NSUserNotificationDefaultSoundName
        }

        center.scheduleNotification(notification)
        return notification
    }

    // Delegate callbacks
    func userNotificationCenter(_ center: NSUserNotificationCenter, shouldPresent notification: NSUserNotification) -> Bool {
        return true
    }

    // Delegate callback for the user notification action.
    func userNotificationCenter(_ center: NSUserNotificationCenter, didActivate notification: NSUserNotification) {
        if notification.activationType == .actionButtonClicked {
            extendLease()
        }
    }
    
    // Checks the current reservation for impending expiration.
    // If expiration is imminent, it allows user to extend the reservation.
    func checkForExpiration() {
        request("\(wardenUrl)/data?user=\(username)", method: "GET", stringData: nil, callback: { (data) in
            let record = data.trimmingCharacters(in: CharacterSet.newlines)
            let userHasReservation = !record.hasPrefix("null")
            if userHasReservation {
                var fields = record.components(separatedBy: ",")
                let remaining = fields.count > 3 ? Int(fields[3]) : 0
                if remaining != nil && remaining < self.warnMinutes && !self.pendingAction {
                    self.showNotification("Cell reservation is about to expire",
                        text: "You have less than \(remaining! + 1) minutes left", action: "Extend", sound: true)
                } else if remaining != nil && !self.hadReservation && !self.pendingAction {
                    self.notification = self.showNotification("Cell is allocated and ready",
                        text: "Reservation is valid for \(remaining!) minutes", action: nil, sound: false)
                    self.scheduleNotificationDismissal()
                }
                
            } else if self.hadReservation {
                self.showNotification("Cell reservation expired", text: "The cell has been returned", action: nil, sound: true)
            }
            self.setHaveReservation(userHasReservation)
            }, errorCallback: {})
    }

    // Checks the current reservation status.
    func checkStatus() {
        request("\(wardenUrl)/data?user=\(username)", method: "GET", stringData: nil, callback: { (data) in
            let record = data.trimmingCharacters(in: CharacterSet.newlines)
            self.setHaveReservation(!record.hasPrefix("null"))
            }, errorCallback: {})
    }
    
    // Sets the indicator and internal state to indicate presence of absence of an active reservation
    func setHaveReservation(_ value: Bool) {
        hadReservation = value
        button?.image = NSImage(named: value ? "Image-Reservation" : "Image")
        button?.image?.isTemplate = true
    }

    // Issues a web-request against the specified URL
    func request(_ urlPath: String, method: String, stringData: String?, callback: @escaping (NSString) -> Void, errorCallback: @escaping () -> Void) {
        let url: URL = URL(string: urlPath)!
        let request = NSMutableURLRequest(url: url)
        request.httpMethod = method
        request.httpBody = stringData?.data(using: String.Encoding.utf8)
        let task = URLSession.shared.dataTask(with: request as URLRequest, completionHandler: { data, response, error in
            guard error == nil && data != nil else {
                print("error = \(error)")
                errorCallback()
                // self.button?.image = NSImage(named: "Image-Offline")
                return
            }
            if let httpStatus = response as? HTTPURLResponse , httpStatus.statusCode != 200 {
                print("status = \(httpStatus.statusCode)\nresponse = \(response)")
            }
            
            callback(NSString(data: data!, encoding: String.Encoding.utf8.rawValue)!)
        }) 
        task.resume()
    }
}

