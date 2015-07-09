/*
* Archon Ship Server
* Copyright (C) 2014 Andrew Rodman
*
* This program is free software: you can redistribute it and/or modify
* it under the terms of the GNU General Public License as published by
* the Free Software Foundation, either version 3 of the License, or
* (at your option) any later version.
*
* This program is distributed in the hope that it will be useful,
* but WITHOUT ANY WARRANTY; without even the implied warranty of
* MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
* GNU General Public License for more details.
*
* You should have received a copy of the GNU General Public License
* along with this program.  If not, see <http://www.gnu.org/licenses/>.
* ---------------------------------------------------------------------
 */
package main

import (
	"fmt"
	"ship_server/ship_server"
)

func main() {
	fmt.Println("Archon Ship Server, Copyright (C) 2014 Andrew Rodman")
	fmt.Println("=====================================================")
	fmt.Println("This program is free software: you can redistribute it and/or\n" +
		"modify it under the terms of the GNU General Public License as\n" +
		"published by the Free Software Foundation, either version 3 of\n" +
		"the License, or (at your option) any later version.")
	fmt.Println("This program is distributed WITHOUT ANY WARRANTY; See LICENSE for details.\n")
	ship_server.StartServer()
}