/*
* Archon Login Server
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
*
* CHARACTER server logic.
 */
package login_server

import (
	"database/sql"
	"fmt"
	"hash/crc32"
	"libarchon/logger"
	"libarchon/util"
)

// Cached parameter data to avoid computing it every time.
var paramHeaderData []byte
var paramChunkData map[int][]byte

// Possible character classes as defined by the game.
type CharClass uint8

const (
	Humar     CharClass = 0x00
	Hunewearl           = 0x01
	Hucast              = 0x02
	Ramar               = 0x03
	Racast              = 0x04
	Racaseal            = 0x05
	Fomarl              = 0x06
	Fonewm              = 0x07
	Fonewearl           = 0x08
	Hucaseal            = 0x09
	Fomar               = 0x0A
	Ramarl              = 0x0B
)

// Struct for caching the parameter chunk data and header so
// that the param files aren't re-read every time.
type parameterEntry struct {
	Size     uint32
	Checksum uint32
	Offset   uint32
	Filename [0x40]uint8
}

// Per-player friend guildcard entries.
type GuildcardEntry struct {
	Guildcard   uint32
	Name        [24]uint16
	TeamName    [16]uint16
	Description [88]uint16
	Reserved    uint8
	Language    uint8
	SectionID   uint8
	CharClass   uint8
	padding     uint32
	Comment     [88]uint16
}

// Per-player guildcard data chunk.
type GuildcardData struct {
	Unknown  [0x114]uint8
	Blocked  [0x1DE8]uint8 //This should be a struct once implemented
	Unknown2 [0x78]uint8
	Entries  [104]GuildcardEntry
	Unknown3 [0x1BC]uint8
}

// Struct used by Character Info packet.
type CharacterPreview struct {
	Experience     uint32
	Level          uint32
	GuildcardStr   [16]byte
	Unknown        [2]uint32
	NameColor      uint32
	Model          byte
	Padding        [15]byte
	NameColorChksm uint32
	SectionId      byte
	Class          byte
	V2flags        byte
	Version        byte
	V1Flags        uint32
	Costume        uint16
	Skin           uint16
	Face           uint16
	Head           uint16
	Hair           uint16
	HairRed        uint16
	HairGreen      uint16
	HairBlue       uint16
	PropX          float32
	PropY          float32
	Name           [24]uint8
	Playtime       uint32
}

// Per-character stats.
type CharacterStats struct {
	ATP uint16
	MST uint16
	EVP uint16
	HP  uint16
	DFP uint16
	ATA uint16
	LCK uint16
}

// Handle initial login - verify the account and send security data.
func handleCharLogin(client *LoginClient) error {
	_, err := verifyAccount(client)
	if err != nil {
		return err
	}
	SendSecurity(client, BBLoginErrorNone, client.guildcard, client.teamId)
	if client.config.CharSelected == 1 {
		SendTimestamp(client)
		SendShipList(client, shipList)
		SendScrollMessage(client)
	}
	return nil
}

// Handle the options request - load key config and other option data from the
// datebase or provide defaults for new accounts.
func handleKeyConfig(client *LoginClient) error {
	optionData := make([]byte, 420)
	archondb := GetConfig().Database()

	row := archondb.QueryRow(
		"SELECT key_config from player_options where guildcard = ?", client.guildcard)
	err := row.Scan(&optionData)
	if err == sql.ErrNoRows {
		// We don't have any saved key config - give them the defaults.
		copy(optionData[:420], BaseKeyConfig[:])
		_, err = archondb.Exec("INSERT INTO player_options (guildcard, key_config) "+
			" VALUES (?, ?)", client.guildcard, optionData[:420])
	}
	if err != nil {
		log.DBError(err.Error())
		return err
	}
	SendOptions(client, optionData)
	return nil
}

// Handle the character select/preview request. Will either return information
// about a character given a particular slot in via 0xE5 response or ack the
// selection with an 0xE4 (also used for an empty slot).
func handleCharacterSelect(client *LoginClient) error {
	var pkt CharSelectionPacket
	util.StructFromBytes(client.recvData[:], &pkt)
	prev := new(CharacterPreview)

	// Character preview request.
	archondb := GetConfig().Database()
	var gc, name []uint8
	row := archondb.QueryRow("SELECT experience, level, guildcard_str, "+
		" name_color, name_color_chksm, model, section_id, char_class, "+
		"v2_flags, version, v1_flags, costume, skin, face, head, hair, "+
		"hair_red, hair_green, hair_blue, proportion_x, proportion_y, "+
		"name, playtime FROM characters WHERE guildcard = ? AND slot_num = ?",
		client.guildcard, pkt.Slot)
	err := row.Scan(&prev.Experience, &prev.Level, &gc,
		&prev.NameColor, &prev.NameColorChksm, &prev.Model, &prev.SectionId,
		&prev.Class, &prev.V2flags, &prev.Version, &prev.V1Flags, &prev.Costume,
		&prev.Skin, &prev.Face, &prev.Head, &prev.Hair, &prev.HairRed,
		&prev.HairGreen, &prev.HairBlue, &prev.PropX, &prev.PropY,
		&name, &prev.Playtime)

	if err == sql.ErrNoRows {
		// We don't have a character for this slot.
		SendCharacterAck(client, pkt.Slot, 2)
		return nil
	} else if err != nil {
		log.DBError(err.Error())
		return err
	}

	if pkt.Selecting == 0x01 {
		// They've selected a character from the menu.
		client.config.CharSelected = 1
		client.config.SlotNum = uint8(pkt.Slot)
		SendSecurity(client, BBLoginErrorNone, client.guildcard, client.teamId)
		SendCharacterAck(client, pkt.Slot, 1)
	} else {
		// They have a character in that slot; send the character preview.
		copy(prev.GuildcardStr[:], gc[:])
		copy(prev.Name[:], name[:])
		SendCharacterPreview(client, prev)
	}
	return nil
}

// Load the player's saved guildcards, build the chunk data, and
// send the chunk header.
func handleGuildcardDataStart(client *LoginClient) error {
	archondb := GetConfig().Database()
	rows, err := archondb.Query(
		"SELECT friend_gc, name, team_name, description, language, "+
			"section_id, char_class, comment FROM guildcard_entries "+
			"WHERE guildcard = ?", client.guildcard)
	if err != nil {
		log.DBError(err.Error())
		return err
	}
	defer rows.Close()
	gcData := new(GuildcardData)

	// Maximum of 140 entries can be sent.
	for i := 0; rows.Next() && i < 140; i++ {
		// TODO: This may not actually work yet, but I haven't gotten to
		// figuring out how this is used yet.
		var name, teamName, desc, comment []uint8
		entry := &gcData.Entries[i]
		err = rows.Scan(&entry.Guildcard, &name, &teamName, &desc,
			&entry.Language, &entry.SectionID, &entry.CharClass, &comment)
		if err != nil {
			log.DBError(err.Error())
			return err
		}
	}
	var size int
	client.gcData, size = util.BytesFromStruct(gcData)
	checksum := crc32.ChecksumIEEE(client.gcData)
	client.gcDataSize = uint16(size)

	SendGuildcardHeader(client, checksum, client.gcDataSize)
	return nil
}

// Send another chunk of the client's guildcard data.
func handleGuildcardChunk(client *LoginClient) {
	var chunkReq GuildcardChunkReqPacket
	util.StructFromBytes(client.recvData[:], &chunkReq)
	if chunkReq.Continue != 0x01 {
		// Cancelled sending guildcard chunks.
		return
	}
	SendGuildcardChunk(client, chunkReq.ChunkRequested)
}

// Create or update a character in a slot.
func handleCharacterUpdate(client *LoginClient) error {
	var charPkt CharPreviewPacket
	charPkt.Character = new(CharacterPreview)
	util.StructFromBytes(client.recvData[:], &charPkt)
	prev := charPkt.Character

	archonDB := GetConfig().Database()
	if client.flag == 0x02 {
		// Player is using the dressing room; update the character. Messy
		// query, but unavoidable if we don't want to be stuck with blobs.
		_, err := archonDB.Exec("UPDATE characters SET name_color=?, model=?, "+
			"name_color_chksm=?, section_id=?, char_class=?, costume=?, skin=?, "+
			"head=?, hair_red=?, hair_green=?, hair_blue,=? proportion_x=?, "+
			"proportion_y=?, name=? WHERE guildcard = ? AND slot_num = ?",
			prev.NameColor, prev.Model, prev.NameColorChksm, prev.SectionId,
			prev.Class, prev.Costume, prev.Skin, prev.Head, prev.HairRed,
			prev.HairGreen, prev.HairBlue, prev.Name[:], prev.PropX, prev.PropY,
			client.guildcard, charPkt.Slot)
		if err != nil {
			log.DBError(err.Error())
			return err
		}
	} else {
		// Delete a character if it already exists.
		_, err := archonDB.Exec("DELETE FROM characters WHERE "+
			"guildcard = ? AND slot_num = ?", client.guildcard, charPkt.Slot)
		if err != nil {
			log.DBError(err.Error())
			return err
		}
		// Grab our base stats for this character class.
		stats := BaseStats[prev.Class]

		// TODO: Set up the default inventory and techniques.
		meseta := 300

		/* TODO: Add the rest of these.
		--unsigned char keyConfig[232]; // 0x3E8 - 0x4CF;
		--techniques blob,
		--options blob,
		*/

		// Create the new character.
		_, err = archonDB.Exec("INSERT INTO characters VALUES (?, ?, 0, 1, ?, "+
			"?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, "+
			"?, ?, ?, ?, ?, ?, 0, 0)", client.guildcard, charPkt.Slot,
			prev.GuildcardStr[:], prev.NameColor, prev.Model, prev.NameColorChksm,
			prev.SectionId, prev.Class, prev.V2flags, prev.Version, prev.V1Flags,
			prev.Costume, prev.Skin, prev.Face, prev.Head, prev.Hair, prev.HairRed,
			prev.HairGreen, prev.HairBlue, prev.PropX, prev.PropY, prev.Name[:],
			stats.ATP, stats.MST, stats.EVP, stats.HP, stats.DFP, stats.ATA,
			stats.LCK, meseta)
		if err != nil {
			log.DBError(err.Error())
			return err
		}
	}

	// Send the security packet with the updated state and slot number so that
	// we know a character has been selected.
	client.config.CharSelected = 1
	client.config.SlotNum = uint8(charPkt.Slot)
	SendSecurity(client, BBLoginErrorNone, client.guildcard, client.teamId)

	SendCharacterAck(client, charPkt.Slot, 0)
	return nil
}

// Player selected one of the items on the ship select screen.
func handleMenuSelect(client *LoginClient) {

}

// Process packets sent to the CHARACTER port by sending them off to another
// handler or by taking some brief action.
func processCharacterPacket(client *LoginClient) error {
	var pktHeader BBPktHeader
	util.StructFromBytes(client.recvData[:BBHeaderSize], &pktHeader)

	if GetConfig().DebugMode {
		fmt.Printf("Got %v bytes from client:\n", pktHeader.Size)
		util.PrintPayload(client.recvData, int(pktHeader.Size))
		fmt.Println()
	}

	var err error = nil
	switch pktHeader.Type {
	case LoginType:
		err = handleCharLogin(client)
	case DisconnectType:
		// Just wait until we recv 0 from the client to d/c.
		break
	case OptionsRequestType:
		err = handleKeyConfig(client)
	case CharPreviewReqType:
		err = handleCharacterSelect(client)
	case ChecksumType:
		// Everybody else seems to ignore this, so...
		SendChecksumAck(client, 1)
	case GuildcardReqType:
		err = handleGuildcardDataStart(client)
	case GuildcardChunkReqType:
		handleGuildcardChunk(client)
	case ParameterHeaderReqType:
		SendParameterHeader(client, uint32(len(ParamFiles)), paramHeaderData)
	case ParameterChunkReqType:
		var pkt BBPktHeader
		util.StructFromBytes(client.recvData[:], &pkt)
		SendParameterChunk(client, paramChunkData[int(pkt.Flags)], pkt.Flags)
	case SetFlagType:
		var pkt SetFlagPacket
		util.StructFromBytes(client.recvData[:], &pkt)
		client.flag = pkt.Flag
	case CharPreviewType:
		err = handleCharacterUpdate(client)
	case MenuSelectType:
		handleMenuSelect(client)
	default:
		msg := fmt.Sprintf("Received unknown packet %x from %s", pktHeader.Type, client.ipAddr)
		log.Info(msg, logger.LogPriorityMedium)
	}
	return err
}
